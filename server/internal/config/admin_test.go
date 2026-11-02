package config_test

import (
	"github.com/laminara/laminara/server/internal/config"
	"testing"
)

func TestRemoteAdminRequiresTLSOutsideLoopback(t *testing.T) {
	for _, test := range []struct {
		addr     string
		insecure bool
		allowed  bool
	}{{"0.0.0.0:8443", false, false}, {"0.0.0.0:8443", true, false}, {"127.0.0.1:8443", false, false}, {"127.0.0.1:8443", true, true}, {"[::1]:8443", true, true}, {"", false, true}} {
		err := config.Validate(&config.Config{Admin: &config.AdminConfig{Addr: test.addr, AllowInsecureLoopback: test.insecure}})
		if (err == nil) != test.allowed {
			t.Fatalf("%+v: %v", test, err)
		}
	}
}
