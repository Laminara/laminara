package clientaddr

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

var defaults = []string{
	"127.0.0.0/8",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"fc00::/7",
}

type Trust struct {
	networks []netip.Prefix
}

func New(cidrs []string) (*Trust, error) {
	if cidrs == nil {
		cidrs = defaults
	}
	trust := &Trust{}
	for _, raw := range cidrs {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		if !strings.Contains(text, "/") {
			address, err := netip.ParseAddr(text)
			if err != nil {
				return nil, fmt.Errorf("api.trustedProxies: %q не адрес и не подсеть", raw)
			}
			trust.networks = append(trust.networks, netip.PrefixFrom(address, address.BitLen()))
			continue
		}
		network, err := netip.ParsePrefix(text)
		if err != nil {
			return nil, fmt.Errorf("api.trustedProxies: %q не подсеть: %w", raw, err)
		}
		trust.networks = append(trust.networks, network)
	}
	return trust, nil
}

func (t *Trust) trusted(address string) bool {
	if t == nil || len(t.networks) == 0 {
		return false
	}
	parsed, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	parsed = parsed.Unmap()
	for _, network := range t.networks {
		if network.Contains(parsed) {
			return true
		}
	}
	return false
}

func (t *Trust) Of(header http.Header, peer string) string {
	direct := hostOf(peer)
	if !t.trusted(direct) {
		return direct
	}

	hops := forwarded(header)
	for i := len(hops) - 1; i >= 0; i-- {
		if !t.trusted(hops[i]) {
			return hops[i]
		}
	}
	if len(hops) > 0 {
		return hops[0]
	}
	if real := strings.TrimSpace(header.Get("X-Real-IP")); real != "" {
		return real
	}
	return direct
}

func (t *Trust) OfRequest(r *http.Request) string {
	return t.Of(r.Header, r.RemoteAddr)
}

func forwarded(header http.Header) []string {
	var hops []string
	for _, value := range header.Values("X-Forwarded-For") {
		for _, hop := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(hop); trimmed != "" {
				hops = append(hops, trimmed)
			}
		}
	}
	return hops
}

func hostOf(peer string) string {
	if host, _, err := net.SplitHostPort(peer); err == nil {
		return host
	}
	return strings.TrimSpace(peer)
}
