package crash

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func init() {
	RegisterSink("discord", newDiscord)
	RegisterSink("telegram", newTelegram)
	RegisterSink("http", newWebhook)
	RegisterSink("file", newFile)
}

func client() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

type discordSink struct {
	url      string
	username string
}

func newDiscord(raw json.RawMessage) (Sink, error) {
	var cfg struct {
		URL      string `json:"url"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("для discord нужен адрес вебхука")
	}
	return &discordSink{url: cfg.URL, username: cfg.Username}, nil
}

func (d *discordSink) Name() string { return "discord" }

func (d *discordSink) Send(ctx context.Context, report Report) error {
	payload := map[string]any{
		"content": "**" + report.Title() + "**\n```\n" + report.Text() + "\n```",
	}
	if d.username != "" {
		payload["username"] = d.username
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	if err := writer.WriteField("payload_json", string(body)); err != nil {
		return err
	}
	if log := strings.TrimSpace(report.Log); log != "" {
		part, err := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": []string{`form-data; name="files[0]"; filename="crash.log"`},
			"Content-Type":        []string{"text/plain; charset=utf-8"},
		})
		if err != nil {
			return err
		}
		if _, err := io.WriteString(part, log); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, &form)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return send(request)
}

type telegramSink struct {
	token string
	chat  string
}

func newTelegram(raw json.RawMessage) (Sink, error) {
	var cfg struct {
		Token  string `json:"token"`
		ChatID string `json:"chatId"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Token == "" || cfg.ChatID == "" {
		return nil, fmt.Errorf("для telegram нужны token и chatId")
	}
	return &telegramSink{token: cfg.Token, chat: cfg.ChatID}, nil
}

func (t *telegramSink) Name() string { return "telegram" }

func (t *telegramSink) Send(ctx context.Context, report Report) error {
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)

	caption := report.Title() + "\n\n" + report.Text()
	if len([]rune(caption)) > 1000 {
		caption = string([]rune(caption)[:1000])
	}
	for field, value := range map[string]string{"chat_id": t.chat, "caption": caption} {
		if err := writer.WriteField(field, value); err != nil {
			return err
		}
	}

	log := strings.TrimSpace(report.Log)
	if log == "" {
		log = "журнал пуст"
	}
	part, err := writer.CreateFormFile("document", "crash.log")
	if err != nil {
		return err
	}
	if _, err := io.WriteString(part, log); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", t.token)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &form)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return send(request)
}

type webhookSink struct {
	url     string
	headers map[string]string
}

func newWebhook(raw json.RawMessage) (Sink, error) {
	var cfg struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("для http нужен адрес")
	}
	return &webhookSink{url: cfg.URL, headers: cfg.Headers}, nil
}

func (w *webhookSink) Name() string { return "http" }

func (w *webhookSink) Send(ctx context.Context, report Report) error {
	body, err := json.Marshal(map[string]any{
		"player":   report.Player,
		"uuid":     report.UUID,
		"build":    report.Build,
		"version":  report.Version,
		"loader":   report.Loader,
		"exitCode": report.ExitCode,
		"platform": report.Platform,
		"os":       report.OSVersion,
		"launcher": report.Launcher,
		"happened": report.Happened,
		"details":  report.Details,
		"log":      report.Log,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	for key, value := range w.headers {
		request.Header.Set(key, value)
	}
	return send(request)
}

type fileSink struct {
	dir string
}

func newFile(raw json.RawMessage) (Sink, error) {
	var cfg struct {
		Dir string `json:"dir"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Dir == "" {
		return nil, fmt.Errorf("для file нужна папка")
	}
	return &fileSink{dir: cfg.Dir}, nil
}

func (f *fileSink) Name() string { return "file" }

func (f *fileSink) Send(_ context.Context, report Report) error {
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		return err
	}
	moment := report.Happened
	if moment.IsZero() {
		moment = time.Now()
	}
	name := fmt.Sprintf("%s-%s.log", moment.Format("20060102-150405"), safe(report.Player))
	body := report.Title() + "\n\n" + report.Text() + "\n\n" + strings.TrimSpace(report.Log) + "\n"
	return os.WriteFile(filepath.Join(f.dir, name), []byte(body), 0o640)
}

func safe(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, value)
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}

func send(request *http.Request) error {
	response, err := client().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("%s ответил %s: %s", request.URL.Host, response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
