package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func init() {
	RegisterBackend("s3", newS3)
}

var _ Backend = (*s3Backend)(nil)

type s3Config struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	PathStyle       bool   `json:"pathStyle"`
	Secure          *bool  `json:"secure"`
}

type s3Backend struct {
	client *minio.Client
	bucket string
}

func newS3(raw json.RawMessage) (Backend, error) {
	var cfg s3Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, errors.New("s3 storage requires an endpoint and bucket")
	}
	host := cfg.Endpoint
	secure := true
	if cfg.Secure != nil {
		secure = *cfg.Secure
	}
	if parsed, err := url.Parse(cfg.Endpoint); err == nil && parsed.Host != "" {
		host = parsed.Host
		switch parsed.Scheme {
		case "http":
			secure = false
		case "https":
			secure = true
		}
	}
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       secure,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, err
	}
	return &s3Backend{client: client, bucket: cfg.Bucket}, nil
}

func (b *s3Backend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := b.client.PutObject(ctx, b.bucket, key, r, size, minio.PutObjectOptions{})
	return err
}

func (b *s3Backend) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return b.client.GetObject(ctx, b.bucket, key, minio.GetObjectOptions{})
}

func (b *s3Backend) Stat(ctx context.Context, key string) (int64, bool, error) {
	info, err := b.client.StatObject(ctx, b.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		response := minio.ToErrorResponse(err)
		if response.StatusCode == http.StatusNotFound || response.Code == "NoSuchKey" {
			return 0, false, nil
		}
		return 0, false, err
	}
	return info.Size, true, nil
}

func (b *s3Backend) Delete(ctx context.Context, key string) error {
	return b.client.RemoveObject(ctx, b.bucket, key, minio.RemoveObjectOptions{})
}

func (b *s3Backend) Locate(ctx context.Context, key string, ttl time.Duration) (Location, error) {
	if ttl <= 0 {
		ttl = time.Hour
	}
	signed, err := b.client.PresignedGetObject(ctx, b.bucket, key, ttl, url.Values{})
	if err != nil {
		return Location{}, err
	}
	return Location{Kind: LocationURL, URL: signed.String()}, nil
}
