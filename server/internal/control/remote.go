package control

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

type ClientOptions struct {
	URL       string
	Token     string
	TokenFile string
	CAFile    string
}

func NewClient(options ClientOptions) (adminv1connect.AdminServiceClient, error) {
	if options.URL == "" {
		return adminv1connect.NewAdminServiceClient(HTTPClient(), BaseURL()), nil
	}
	endpoint, err := url.Parse(options.URL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("нужен URL административного слушателя без логина, query и fragment")
	}
	if endpoint.Scheme != "https" {
		ip := net.ParseIP(endpoint.Hostname())
		if endpoint.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("удалённый административный URL должен использовать HTTPS")
		}
	}
	token := options.Token
	if options.TokenFile != "" {
		data, err := os.ReadFile(options.TokenFile)
		if err != nil {
			return nil, errors.New("не удалось прочитать файл административного токена")
		}
		token = strings.TrimSpace(string(data))
	}
	if token == "" || len(token) > 512 || strings.ContainsAny(token, " \r\n\t") {
		return nil, errors.New("задайте административный токен через файл или LAMINARA_ADMIN_TOKEN")
	}
	security := &tls.Config{MinVersion: tls.VersionTLS12}
	if options.CAFile != "" {
		data, err := os.ReadFile(options.CAFile)
		if err != nil {
			return nil, err
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(data) {
			return nil, errors.New("CA файл не содержит сертификатов")
		}
		security.RootCAs = roots
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSClientConfig: security, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 90 * time.Second, ForceAttemptHTTP2: true}
	client := &http.Client{Transport: bearerTransport{next: transport, token: token}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return adminv1connect.NewAdminServiceClient(client, strings.TrimRight(options.URL, "/")), nil
}

type bearerTransport struct {
	next  http.RoundTripper
	token string
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+t.token)
	return t.next.RoundTrip(request)
}
