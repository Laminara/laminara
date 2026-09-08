package redisconf

import (
	"crypto/tls"
	"net"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Addr     string `json:"addr"`
	Password string `json:"password"`
	DB       int    `json:"db"`
	TLS      bool   `json:"tls"`
}

func (c Config) WithFallbackAddr(addr string) Config {
	if strings.TrimSpace(c.Addr) == "" {
		c.Addr = strings.TrimSpace(addr)
	}
	return c
}

func (c Config) Set() bool {
	return strings.TrimSpace(c.Addr) != ""
}

func (c Config) Client() *redis.Client {
	options := &redis.Options{
		Addr:            strings.TrimSpace(c.Addr),
		Password:        c.Password,
		DB:              c.DB,
		ConnMaxIdleTime: time.Minute,
		ConnMaxLifetime: 3 * time.Minute,
	}
	if c.TLS {
		host, _, err := net.SplitHostPort(options.Addr)
		if err != nil {
			host = options.Addr
		}
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	}
	return redis.NewClient(options)
}
