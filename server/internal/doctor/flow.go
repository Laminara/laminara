package doctor

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/gen/go/laminara/api/v1/apiv1connect"
	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/storage"
)

func checkFlow(ctx context.Context, opts Options, probe *diag.Probe) {
	if opts.Username == "" || opts.Password == "" {
		probe.Skip("путь игрока", "не задан аккаунт — запустите с --as <ник>, чтобы пройти вход целиком")
		return
	}
	base, stop, err := reachableBase(opts)
	if err != nil {
		probe.Fail("путь игрока", fmt.Sprintf("некуда обращаться: %v", err), diag.Remedy{
			Hint: "проверьте api.addr и launcher.endpoints",
		})
		return
	}
	if stop != nil {
		defer stop()
	}
	client := apiv1connect.NewLauncherServiceClient(&http.Client{Timeout: 30 * time.Second}, base)

	tokens := checkLogin(ctx, opts, probe, client, base)
	if tokens == nil {
		return
	}
	if checkRefresh(ctx, probe, client, tokens) {
		fresh := signIn(ctx, opts, client)
		if fresh == nil {
			probe.Fail("повторный вход", "после проверки защиты от кражи войти заново не вышло", diag.Remedy{
				Hint: "сессия отзывается намеренно, но следующий вход обязан проходить — смотрите разделы аккаунтов выше",
			})
			return
		}
		tokens = fresh
	}
	checkCatalogFlow(ctx, opts, probe, client, tokens)
	checkYggdrasilFlow(ctx, opts, probe, base)
}

func reachableBase(opts Options) (string, func(), error) {
	if opts.Endpoint != "" {
		return strings.TrimRight(opts.Endpoint, "/"), nil, nil
	}
	if opts.Running {
		if opts.Config.API == nil || opts.Config.API.Addr == "" {
			return "", nil, errors.New("не задан api.addr")
		}
		return "http://" + dialable(opts.Config.API.Addr), nil, nil
	}
	if opts.Wired == nil || opts.Wired.PublicHandler == nil {
		return "", nil, errors.New("публичный обработчик не собран")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := &http.Server{Handler: opts.Wired.PublicHandler}
	go server.Serve(listener)
	return "http://" + listener.Addr().String(), func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}, nil
}

func checkLogin(ctx context.Context, opts Options, probe *diag.Probe, client apiv1connect.LauncherServiceClient, base string) *apiv1.Tokens {
	started := time.Now()
	response, err := client.Login(ctx, connect.NewRequest(&apiv1.LoginRequest{
		Username: opts.Username,
		Password: opts.Password,
	}))
	if err != nil {
		probe.Fail("вход", fmt.Sprintf("%s: %v", opts.Username, err), diag.Remedy{
			Hint: loginHint(err),
		})
		return nil
	}
	if response.Msg.Tokens == nil || response.Msg.Tokens.Access == "" {
		probe.Fail("вход", "сервер ответил без токена", diag.Remedy{
			Hint: "лаунчер получит пустой ответ и не сможет продолжить",
		})
		return nil
	}
	probe.OK("вход", "%s вошёл через %s за %s", opts.Username, base, time.Since(started).Round(time.Millisecond))
	return response.Msg.Tokens
}

func loginHint(err error) string {
	if connect.CodeOf(err) == connect.CodeUnauthenticated {
		return "проверьте ник и пароль; если они верны, смотрите раздел аккаунтов выше — чаще всего расходится схема хеширования"
	}
	return "сервер не довёл вход до конца; смотрите разделы аккаунтов и публичного доступа выше"
}

func signIn(ctx context.Context, opts Options, client apiv1connect.LauncherServiceClient) *apiv1.Tokens {
	response, err := client.Login(ctx, connect.NewRequest(&apiv1.LoginRequest{
		Username: opts.Username,
		Password: opts.Password,
	}))
	if err != nil || response.Msg.Tokens == nil {
		return nil
	}
	return response.Msg.Tokens
}

func checkRefresh(ctx context.Context, probe *diag.Probe, client apiv1connect.LauncherServiceClient, tokens *apiv1.Tokens) bool {
	if tokens.Refresh == "" {
		probe.Warn("продление входа", "сервер не выдал токен продления", diag.Remedy{
			Hint: "игрока будет выкидывать из лаунчера, как только истечёт короткий токен",
		})
		return false
	}
	refreshed, err := client.Refresh(ctx, connect.NewRequest(&apiv1.RefreshRequest{Refresh: tokens.Refresh}))
	if err != nil {
		probe.Fail("продление входа", fmt.Sprintf("не работает: %v", err), diag.Remedy{
			Hint: "долгая установка сборки оборвётся на середине, когда истечёт короткий токен",
		})
		return false
	}
	probe.OK("продление входа", "работает")

	if _, err := client.Refresh(ctx, connect.NewRequest(&apiv1.RefreshRequest{Refresh: tokens.Refresh})); err == nil {
		probe.Fail("защита от кражи токена", "старый токен продления приняли второй раз", diag.Remedy{
			Hint: "украденный токен останется рабочим навсегда; так быть не должно — сообщите об этом разработчику",
		})
		tokens.Access = refreshed.Msg.Tokens.Access
		tokens.Refresh = refreshed.Msg.Tokens.Refresh
		return false
	}
	probe.OK("защита от кражи токена", "повторное продление старым токеном отклонено")
	return true
}

func checkCatalogFlow(ctx context.Context, opts Options, probe *diag.Probe, client apiv1connect.LauncherServiceClient, tokens *apiv1.Tokens) {
	request := connect.NewRequest(&apiv1.ListProfilesRequest{Platform: corev1.Platform_PLATFORM_UNSPECIFIED})
	request.Header().Set("Authorization", "Bearer "+tokens.Access)
	profiles, err := client.ListProfiles(ctx, request)
	if err != nil {
		probe.Fail("список сборок", fmt.Sprintf("не отдаётся: %v", err), diag.Remedy{
			Hint: "игрок войдёт, но увидит пустой лаунчер",
		})
		return
	}
	if len(profiles.Msg.Profiles) == 0 {
		probe.Warn("список сборок", "игроку не показывается ни одна сборка", diag.Remedy{
			Hint: "опубликуйте сборку или проверьте правила доступа — они могли скрыть всё",
		})
		return
	}
	probe.OK("список сборок", "игрок видит %d", len(profiles.Msg.Profiles))

	profile := profiles.Msg.Profiles[0]
	name := profile.Name
	target := corev1.Platform_PLATFORM_UNSPECIFIED
	if len(profile.Platforms) > 0 {
		target = profile.Platforms[0]
	}
	manifestRequest := connect.NewRequest(&apiv1.GetManifestRequest{
		Profile:  name,
		Platform: target,
	})
	manifestRequest.Header().Set("Authorization", "Bearer "+tokens.Access)
	got, err := client.GetManifest(ctx, manifestRequest)
	if err != nil {
		probe.Fail("манифест сборки", fmt.Sprintf("%s не отдаётся: %v", name, err), diag.Remedy{
			Hint: "лаунчер не сможет начать установку",
		})
		return
	}
	if !verifySignature(opts, got.Msg.Manifest, got.Msg.Signature) {
		probe.Fail("подпись манифеста", fmt.Sprintf("%s подписан ключом, которого нет в кольце", name), diag.Remedy{
			Hint: "лаунчер отвергнет такую сборку; опубликуйте её заново текущим ключом",
		})
		return
	}
	probe.OK("манифест сборки", "%s отдан и подписан верно", name)
	checkObjectFetch(ctx, opts, probe, got.Msg.Manifest, tokens)
}

func verifySignature(opts Options, canonical, signature []byte) bool {
	if opts.Wired == nil || opts.Wired.Signing == nil {
		return true
	}
	public, ok := opts.Wired.Signing.Active().Public().(ed25519.PublicKey)
	if !ok {
		return true
	}
	return manifest.Verify(public, canonical, signature)
}

func checkObjectFetch(ctx context.Context, opts Options, probe *diag.Probe, canonical []byte, tokens *apiv1.Tokens) {
	var parsed corev1.Manifest
	if err := proto.Unmarshal(canonical, &parsed); err != nil {
		return
	}
	var key string
	var size uint64
	for _, file := range parsed.Files {
		if file.Object != nil && file.Object.Hash != nil {
			key = storage.ObjectKey(file.Object.Hash.Algo, file.Object.Hash.Value)
			size = file.Object.Size
			break
		}
	}
	if key == "" {
		return
	}
	base, stop, err := reachableBase(opts)
	if err != nil {
		return
	}
	if stop != nil {
		defer stop()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/objects/"+key, nil)
	if err != nil {
		return
	}
	request.Header.Set("Authorization", "Bearer "+tokens.Access)
	response, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
	if err != nil {
		probe.Fail("скачивание файла", fmt.Sprintf("не удалось: %v", err), diag.Remedy{
			Hint: "игрок не сможет скачать сборку; проверьте раздел хранилища и nginx",
		})
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		probe.Fail("скачивание файла", fmt.Sprintf("сервер отвечает %s", response.Status), hintForObject(opts, response.StatusCode))
		return
	}
	written, err := io.Copy(io.Discard, response.Body)
	if err != nil {
		probe.Fail("скачивание файла", fmt.Sprintf("оборвалось: %v", err), diag.Remedy{
			Hint: "проверьте таймауты и буферизацию в nginx",
		})
		return
	}
	if size > 0 && uint64(written) != size {
		probe.Fail("скачивание файла", fmt.Sprintf("получено %d байт вместо %d", written, size), diag.Remedy{
			Hint: "файлы приходят битыми — игрок будет перекачивать сборку снова и снова",
		})
		return
	}
	probe.OK("скачивание файла", "%d байт получены целиком", written)
}

func hintForObject(opts Options, status int) diag.Remedy {
	if status == http.StatusNotFound && opts.Config.API != nil && opts.Config.API.XAccel {
		if _, ok := opts.Wired.Storage.(interface {
			Check(context.Context, *diag.Probe)
		}); ok && opts.Config.Storage != nil && opts.Config.Storage.Backend == "fs" {
			return diag.Remedy{
				Hint: "включён api.xAccel — файлы должен отдавать nginx; проверьте, что внутренняя локация из nginx-config добавлена в конфиг сайта",
			}
		}
	}
	return diag.Remedy{
		Hint: "проверьте раздел хранилища выше и правила доступа к объектам",
	}
}

func checkYggdrasilFlow(ctx context.Context, opts Options, probe *diag.Probe, base string) {
	if opts.Config.Yggdrasil == nil || !opts.Config.Yggdrasil.Enabled {
		return
	}
	payload := map[string]any{
		"username": opts.Username,
		"password": opts.Password,
		"agent":    map[string]any{"name": "Minecraft", "version": 1},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/yggdrasil/authserver/authenticate", strings.NewReader(string(body)))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		probe.Fail("вход в игре", fmt.Sprintf("authserver не отвечает: %v", err), diag.Remedy{
			Hint: "лаунчер считает вход незавершённым, пока не ответит yggdrasil — игрок не войдёт вообще",
		})
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		probe.Fail("вход в игре", fmt.Sprintf("authserver отвечает %s", response.Status), diag.Remedy{
			Hint: "проверьте, что yggdrasil включён и что прокси доводит до сервера путь /yggdrasil/",
		})
		return
	}
	var answer struct {
		AccessToken       string                      `json:"accessToken"`
		SelectedProfile   struct{ ID, Name string }   `json:"selectedProfile"`
		AvailableProfiles []struct{ ID, Name string } `json:"availableProfiles"`
	}
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		probe.Fail("вход в игре", fmt.Sprintf("ответ не разбирается: %v", err), diag.Remedy{
			Hint: "игра ожидает ответ формата Yggdrasil",
		})
		return
	}
	if answer.AccessToken == "" || answer.SelectedProfile.Name == "" {
		probe.Fail("вход в игре", "authserver ответил без профиля игрока", diag.Remedy{
			Hint: "игра не поймёт, кем заходит игрок",
		})
		return
	}
	probe.OK("вход в игре", "профиль %s получен", answer.SelectedProfile.Name)
	checkJoinFlow(ctx, probe, base, answer.AccessToken, answer.SelectedProfile.ID, answer.SelectedProfile.Name)
}

func checkJoinFlow(ctx context.Context, probe *diag.Probe, base, token, profileID, username string) {
	server := fmt.Sprintf("%040d", time.Now().UnixNano()%1e9)
	join, err := json.Marshal(map[string]string{
		"accessToken":     token,
		"selectedProfile": profileID,
		"serverId":        server,
	})
	if err != nil {
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/yggdrasil/sessionserver/session/minecraft/join", strings.NewReader(string(join)))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		probe.Fail("вход на сервер игры", fmt.Sprintf("sessionserver не отвечает: %v", err), diag.Remedy{
			Hint: "игрок войдёт в лаунчер, но не сможет зайти на игровой сервер",
		})
		return
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK {
		probe.Fail("вход на сервер игры", fmt.Sprintf("sessionserver отвечает %s", response.Status), diag.Remedy{
			Hint: "проверьте путь /yggdrasil/ на прокси",
		})
		return
	}
	target := fmt.Sprintf("%s/yggdrasil/sessionserver/session/minecraft/hasJoined?username=%s&serverId=%s", base, username, server)
	verify, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return
	}
	verified, err := (&http.Client{Timeout: 20 * time.Second}).Do(verify)
	if err != nil {
		probe.Fail("вход на сервер игры", fmt.Sprintf("проверка входа не отвечает: %v", err), diag.Remedy{
			Hint: "игровой сервер не сможет подтвердить игрока и отклонит его",
		})
		return
	}
	defer verified.Body.Close()
	if verified.StatusCode != http.StatusOK {
		probe.Fail("вход на сервер игры", fmt.Sprintf("проверка входа отвечает %s", verified.Status), diag.Remedy{
			Hint: "игрока не пустят на сервер с ошибкой проверки сессии",
		})
		return
	}
	probe.OK("вход на сервер игры", "вход подтверждается")
}
