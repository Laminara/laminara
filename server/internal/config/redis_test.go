package config_test

import (
	"encoding/json"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/ratelimit"
)

func TestOldRedisAddrKeepsWorking(t *testing.T) {
	var sessions config.SessionConfig
	if err := json.Unmarshal([]byte(`{"backend":"redis","redisAddr":"10.0.0.5:6379"}`), &sessions); err != nil {
		t.Fatal(err)
	}
	if sessions.Redis.Addr != "10.0.0.5:6379" {
		t.Fatalf("конфиг, написанный до появления пароля, перестал работать: %+v", sessions.Redis)
	}

	var limits ratelimit.Config
	if err := json.Unmarshal([]byte(`{"backend":"redis","redisAddr":"10.0.0.5:6379"}`), &limits); err != nil {
		t.Fatal(err)
	}
	if limits.Redis.Addr != "10.0.0.5:6379" {
		t.Fatalf("счётчики потеряли адрес из старого конфига: %+v", limits.Redis)
	}
}

func TestNewRedisBlockWins(t *testing.T) {
	var sessions config.SessionConfig
	if err := json.Unmarshal([]byte(`{"backend":"redis","redisAddr":"старый:6379","redis":{"addr":"новый:6379","password":"тайна","db":3,"tls":true}}`), &sessions); err != nil {
		t.Fatal(err)
	}
	if sessions.Redis.Addr != "новый:6379" || sessions.Redis.Password != "тайна" || sessions.Redis.DB != 3 || !sessions.Redis.TLS {
		t.Fatalf("новый блок redis прочитан неверно: %+v", sessions.Redis)
	}
}
