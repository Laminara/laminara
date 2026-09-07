package providers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/auth/hash"
	"github.com/laminara/laminara/server/internal/diag"
)

const weakBcryptCost = 10

func (p *sqlProvider) Check(ctx context.Context, probe *diag.Probe) {
	started := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.db.PingContext(pingCtx); err != nil {
		probe.Fail("база пользователей", fmt.Sprintf("%s не отвечает: %v", p.driver, err), diag.Remedy{
			Hint: "проверьте dsn в auth.config.dsn, доступность хоста базы и права пользователя",
		})
		return
	}
	probe.OK("база пользователей", "%s отвечает за %s", p.driver, time.Since(started).Round(time.Millisecond))

	if _, _, _, err := p.lookup(ctx, "laminara-doctor-"+uuid.NewString()); err != nil && !errors.Is(err, auth.ErrInvalidCredentials) {
		probe.Fail("запрос аккаунта", fmt.Sprintf("не выполняется: %v", err), diag.Remedy{
			Hint: p.schemaHint(),
		})
		return
	}
	probe.OK("запрос аккаунта", "%s", p.describeQuery())

	if p.custom {
		probe.Skip("формат паролей", "запрос задан вручную — колонку с паролем определить нельзя")
		return
	}
	p.checkPopulation(ctx, probe)
	p.checkScheme(ctx, probe)
	p.checkUUIDs(ctx, probe)
}

func (p *sqlProvider) describeQuery() string {
	if p.custom {
		return "запрос из auth.config.query выполняется"
	}
	return fmt.Sprintf("таблица %s, колонки %s и %s на месте", p.table, p.usernameCol, p.passwordCol)
}

func (p *sqlProvider) schemaHint() string {
	if p.custom {
		return "проверьте auth.config.query — он должен выбирать пароль и, при желании, uuid"
	}
	return fmt.Sprintf("проверьте, что таблица %q и колонки %q, %q существуют в этой базе", p.table, p.usernameCol, p.passwordCol)
}

func (p *sqlProvider) checkPopulation(ctx context.Context, probe *diag.Probe) {
	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", p.quote(p.table))
	if err := p.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		probe.Warn("аккаунты", fmt.Sprintf("не удалось посчитать: %v", err), diag.Remedy{
			Hint: "дайте пользователю базы право SELECT на эту таблицу",
		})
		return
	}
	if count == 0 {
		probe.Warn("аккаунты", fmt.Sprintf("таблица %s пуста — войти не сможет никто", p.table), diag.Remedy{
			Hint: "заведите игроков в вашей CMS или укажите в auth.config.table таблицу, где они лежат",
		})
		return
	}
	probe.OK("аккаунты", "%d в таблице %s", count, p.table)
}

func (p *sqlProvider) checkScheme(ctx context.Context, probe *diag.Probe) {
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s IS NOT NULL AND %s <> '' LIMIT 1",
		p.quote(p.passwordCol), p.quote(p.table), p.quote(p.passwordCol), p.quote(p.passwordCol))
	var stored string
	if err := p.db.QueryRowContext(ctx, query).Scan(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			probe.Skip("формат паролей", "в колонке %s нет ни одного заполненного значения", p.passwordCol)
			return
		}
		probe.Warn("формат паролей", fmt.Sprintf("не удалось прочитать образец: %v", err), diag.Remedy{
			Hint: "проверьте права на чтение колонки с паролем",
		})
		return
	}
	reportScheme(probe, stored, p.scheme, "auth.config.hash")
}

func (p *sqlProvider) checkUUIDs(ctx context.Context, probe *diag.Probe) {
	if !p.hasUUID {
		probe.Warn("uuid игроков", "колонка с uuid не указана — он будет выводиться из ника", diag.Remedy{
			Hint:    "если в вашей CMS есть колонка с uuid, укажите её, иначе смена ника обнулит прогресс игрока",
			Command: "laminara-server settings auth.config.fields.uuid uuid",
		})
		return
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s IS NOT NULL AND %s <> '' LIMIT 1",
		p.quote(p.uuidCol), p.quote(p.table), p.quote(p.uuidCol), p.quote(p.uuidCol))
	var stored string
	if err := p.db.QueryRowContext(ctx, query).Scan(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			probe.Skip("uuid игроков", "колонка %s пока пуста", p.uuidCol)
			return
		}
		probe.Warn("uuid игроков", fmt.Sprintf("не удалось прочитать образец: %v", err), diag.Remedy{
			Hint: "проверьте права на чтение колонки с uuid",
		})
		return
	}
	if _, err := uuid.Parse(stored); err != nil {
		probe.Warn("uuid игроков", fmt.Sprintf("значение в колонке %s не разбирается как uuid", p.uuidCol), diag.Remedy{
			Hint: "Laminara молча заменит такие значения на uuid, выведенный из ника; приведите колонку к виду 8-4-4-4-12",
		})
		return
	}
	probe.OK("uuid игроков", "колонка %s заполнена корректно", p.uuidCol)
}

func (p *jsonFileProvider) Check(_ context.Context, probe *diag.Probe) {
	info, err := os.Stat(p.path)
	if err != nil {
		probe.Fail("файл аккаунтов", fmt.Sprintf("%s: %v", p.path, err), diag.Remedy{
			Hint: "укажите существующий файл в auth.config.path",
		})
		return
	}
	probe.OK("файл аккаунтов", "%s, аккаунтов %d", p.path, len(p.users))

	if info.Mode().Perm()&0o077 != 0 {
		path := p.path
		probe.Warn("права на файл аккаунтов", fmt.Sprintf("%s открыт остальным (%v) — в нём хеши паролей", path, info.Mode().Perm()), diag.Remedy{
			Hint:    "оставьте 0600",
			Command: fmt.Sprintf("chmod 600 %s", path),
			Apply: func(context.Context) error {
				return os.Chmod(path, 0o600)
			},
		})
	}

	if len(p.users) == 0 {
		probe.Warn("аккаунты", "файл пуст — войти не сможет никто", diag.Remedy{
			Hint: "добавьте записи вида {\"username\":\"…\",\"password\":\"<хеш>\"}",
		})
		return
	}
	for _, record := range p.users {
		stored, _ := record[p.passwordKey].(string)
		if strings.TrimSpace(stored) == "" {
			continue
		}
		reportScheme(probe, stored, p.scheme, "auth.config.hash")
		return
	}
	probe.Skip("формат паролей", "ни в одной записи нет заполненного поля %q", p.passwordKey)
}

func (p *httpProvider) Check(ctx context.Context, probe *diag.Probe) {
	if sendsPasswordsInClear(p.url) {
		probe.Warn("канал до провайдера", fmt.Sprintf("%s — пароли игроков идут открытым текстом", p.url), diag.Remedy{
			Hint: "поднимите TLS и укажите https, либо держите этот API на localhost за общим прокси",
		})
	}
	started := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := p.Authenticate(callCtx, auth.Credentials{
		Username: "laminara-doctor-" + uuid.NewString(),
		Password: uuid.NewString(),
	})
	switch {
	case err == nil:
		probe.Fail("проверка пароля", "провайдер принял несуществующий аккаунт со случайным паролем", diag.Remedy{
			Hint: "ваш API отвечает успехом на любой запрос — войти сможет кто угодно под любым ником; проверьте successField и логику эндпоинта",
		})
	case errors.Is(err, auth.ErrInvalidCredentials):
		probe.OK("проверка пароля", "%s отвечает за %s и отклоняет чужие пароли", p.url, time.Since(started).Round(time.Millisecond))
	default:
		probe.Fail("проверка пароля", fmt.Sprintf("%s: %v", p.url, err), diag.Remedy{
			Hint: "эндпоинт недоступен или отвечает не тем форматом; ожидается JSON с полем успеха",
		})
	}
}

func reportScheme(probe *diag.Probe, stored, configured, settingPath string) {
	detected, ok := hash.Detect(stored)
	if !ok {
		if configured == "plain" {
			probe.Warn("формат паролей", "пароли лежат открытым текстом", diag.Remedy{
				Hint: "переведите хранилище на argon2id или bcrypt — сейчас утечка базы отдаёт готовые пароли",
			})
			return
		}
		probe.Warn("формат паролей", fmt.Sprintf("образец не похож ни на одну известную схему, а в настройках указан %s", configured), diag.Remedy{
			Hint: fmt.Sprintf("проверьте, что колонка с паролем содержит хеши схемы %s", configured),
		})
		return
	}
	if detected.Scheme != configured {
		probe.Fail("формат паролей", fmt.Sprintf("в хранилище %s, а в настройках %s — ни один пароль не подойдёт", detected.Scheme, configured), diag.Remedy{
			Hint:    fmt.Sprintf("укажите схему, которой пользуется ваша CMS: %s", detected.Scheme),
			Command: fmt.Sprintf("laminara-server settings %s %s", settingPath, detected.Scheme),
		})
		return
	}
	if !detected.Supported {
		probe.Fail("формат паролей", fmt.Sprintf("%s не поддерживается", detected.Scheme), diag.Remedy{
			Hint: "Laminara умеет argon2id, bcrypt, sha256, sha512, md5 и plain; для остальных напишите модуль со своим провайдером",
		})
		return
	}
	if reason, weak := hash.Weakness(detected.Scheme); weak {
		probe.Warn("формат паролей", fmt.Sprintf("%s — %s", detected.Scheme, reason), diag.Remedy{
			Hint: "схему задаёт ваша CMS; при возможности переведите её на argon2id или bcrypt",
		})
		return
	}
	if detected.Scheme == "bcrypt" && detected.Cost < weakBcryptCost {
		probe.Warn("формат паролей", fmt.Sprintf("bcrypt со стоимостью %d — это быстрый перебор", detected.Cost), diag.Remedy{
			Hint: fmt.Sprintf("стоимость задаёт ваша CMS при регистрации; сегодня разумный минимум — %d", weakBcryptCost),
		})
		return
	}
	detail := detected.Scheme
	switch {
	case detected.Cost > 0:
		detail = fmt.Sprintf("%s, стоимость %d", detected.Scheme, detected.Cost)
	case detected.Parameters != "":
		detail = fmt.Sprintf("%s, %s", detected.Scheme, detected.Parameters)
	}
	probe.OK("формат паролей", "%s — совпадает с настройками", detail)
}
