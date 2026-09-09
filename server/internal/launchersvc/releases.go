package launchersvc

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

type Releases struct {
	dir string

	mu     sync.Mutex
	cached remembered
}

func NewReleases(dir string) *Releases {
	return &Releases{dir: dir}
}

func (r *Releases) Current() (canonical, signature []byte, err error) {
	if r == nil || r.dir == "" {
		return nil, nil, nil
	}
	stamp, ok := fileStamp(filepath.Join(r.dir, releaseFile))
	if ok {
		if hit, fresh := r.remembered(stamp); fresh {
			return hit.canonical, hit.signature, nil
		}
	}
	canonical, err = os.ReadFile(filepath.Join(r.dir, releaseFile))
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	signature, err = os.ReadFile(filepath.Join(r.dir, signatureFile))
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if ok {
		r.remember(stamp, canonical, signature)
	}
	return canonical, signature, nil
}

type stamp struct {
	changed time.Time
	size    int64
}

type remembered struct {
	canonical []byte
	signature []byte
	seen      stamp
}

func fileStamp(path string) (stamp, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{}, false
	}
	return stamp{changed: info.ModTime(), size: info.Size()}, true
}

func (r *Releases) remembered(now stamp) (remembered, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached.canonical == nil || r.cached.seen != now {
		return remembered{}, false
	}
	return r.cached, true
}

func (r *Releases) remember(seen stamp, canonical, signature []byte) {
	r.mu.Lock()
	r.cached = remembered{canonical: canonical, signature: signature, seen: seen}
	r.mu.Unlock()
}

func (r *Releases) All() ([]*corev1.LauncherRelease, error) {
	canonical, _, err := r.Current()
	if err != nil || len(canonical) == 0 {
		return nil, err
	}
	release, err := Decode(canonical)
	if err != nil {
		return nil, err
	}
	return []*corev1.LauncherRelease{release}, nil
}

func Decode(canonical []byte) (*corev1.LauncherRelease, error) {
	var release corev1.LauncherRelease
	if err := proto.Unmarshal(canonical, &release); err != nil {
		return nil, err
	}
	return &release, nil
}
