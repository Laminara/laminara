package storage_test

import (
	"bytes"
	"context"
	"io"

	"encoding/json"
	"fmt"
	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/laminara/laminara/server/internal/storage"
)

func TestS3CASRepairIntegration(t *testing.T) {
	endpoint := os.Getenv("LAMINARA_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("LAMINARA_TEST_S3_ENDPOINT is not set")
	}
	accessKey, secretKey := os.Getenv("LAMINARA_TEST_S3_ACCESS_KEY"), os.Getenv("LAMINARA_TEST_S3_SECRET_KEY")
	if accessKey == "" || secretKey == "" {
		t.Fatal("S3 test credentials are not set")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		t.Fatal("invalid S3 test endpoint")
	}
	client, err := minio.New(parsed.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: parsed.Scheme == "https", Region: "us-east-1", BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	bucket := "laminara-cas-repair-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		unfinished := make(map[string]struct{})
		for upload := range client.ListIncompleteUploads(ctx, bucket, "", true) {
			if upload.Err != nil {
				t.Error(upload.Err)
				break
			}
			unfinished[upload.Key] = struct{}{}
		}
		for key := range unfinished {
			if err := client.RemoveIncompleteUpload(ctx, bucket, key); err != nil {
				t.Error(err)
			}
		}
		for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			if object.Err != nil {
				t.Error(object.Err)
				break
			}
			if err := client.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
				t.Error(err)
			}
		}
		if err := client.RemoveBucket(ctx, bucket); err != nil {
			t.Error(err)
		}
	})
	raw, err := json.Marshal(map[string]any{
		"endpoint": endpoint, "bucket": bucket, "region": "us-east-1", "pathStyle": true,
		"accessKeyId": accessKey, "secretAccessKey": secretKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := storage.BuildBackend("s3", raw)
	if err != nil {
		t.Fatal(err)
	}
	testCASRepair(t, func(*testing.T) storage.Backend { return backend })
	testCASEmptyRepair(t, backend)
	t.Run("concurrent", func(t *testing.T) { testCASConcurrentRepair(t, backend) })
	t.Run("multipart", func(t *testing.T) {
		payload := bytes.Repeat([]byte{0x5a}, 18<<20)
		cas := storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3)
		first, err := cas.Put(ctx, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		key := storage.ObjectKey(first.Hash.Algo, first.Hash.Value)
		corrupt := bytes.Repeat([]byte{0x33}, len(payload))
		if err := backend.Put(ctx, key, bytes.NewReader(corrupt), int64(len(corrupt))); err != nil {
			t.Fatal(err)
		}
		source := writeSource(t, payload)
		core := minio.Core{Client: client}
		foreign, err := core.NewMultipartUpload(ctx, bucket, key, minio.PutObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		cancelled, cancelUpload := context.WithTimeout(ctx, 3*time.Second)
		interrupted := storage.NewCAS(&interruptedMultipartBackend{Backend: backend}, corev1.HashAlgo_HASH_ALGO_BLAKE3)
		_, uploadError := interrupted.PutFile(cancelled, source)
		wasCancelled := cancelled.Err()
		cancelUpload()
		if uploadError == nil || wasCancelled == nil {
			t.Fatalf("expected interrupted multipart upload, got %v", uploadError)
		}
		old, err := backend.Get(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		previous, readError := io.ReadAll(old)
		old.Close()
		if readError != nil || !bytes.Equal(previous, corrupt) {
			t.Fatalf("interrupted upload changed the old object: %v", readError)
		}
		pending := 0
		for upload := range client.ListIncompleteUploads(ctx, bucket, key, true) {
			if upload.Err != nil {
				t.Fatal(upload.Err)
			}
			if upload.Key == key {
				pending++
				if upload.UploadID != foreign {
					t.Fatalf("own interrupted upload was not cleaned: %s", upload.UploadID)
				}
			}
		}
		if pending != 1 {
			t.Fatalf("parallel upload was removed: got %d uploads, want one", pending)
		}
		if err := core.AbortMultipartUpload(ctx, bucket, key, foreign); err != nil {
			t.Fatal(err)
		}
		ref, err := cas.PutFile(ctx, source)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := cas.Get(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("multipart repair returned wrong content: %v", err)
		}
		if ref.Size != first.Size || !bytes.Equal(ref.Hash.Value, first.Hash.Value) {
			t.Fatal("multipart repair changed the object reference")
		}
	})
}

type interruptedMultipartBackend struct{ storage.Backend }

func (b *interruptedMultipartBackend) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	at, ok := reader.(io.ReaderAt)
	if !ok {
		return fmt.Errorf("multipart upload lost ReaderAt")
	}
	seeker, ok := reader.(io.Seeker)
	if !ok {
		return fmt.Errorf("multipart upload lost Seeker")
	}
	return b.Backend.Put(ctx, key, &interruptedMultipartReader{Reader: reader, at: at, seeker: seeker, ctx: ctx}, size)
}

type interruptedMultipartReader struct {
	io.Reader
	at     io.ReaderAt
	seeker io.Seeker
	ctx    context.Context
}

func (r *interruptedMultipartReader) ReadAt(buffer []byte, offset int64) (int, error) {
	if offset >= 16<<20 {
		<-r.ctx.Done()
		return 0, r.ctx.Err()
	}
	return r.at.ReadAt(buffer, offset)
}

func (r *interruptedMultipartReader) Seek(offset int64, whence int) (int64, error) {
	return r.seeker.Seek(offset, whence)
}
