package launchersvc

import (
	"os"
	"path/filepath"

	"google.golang.org/protobuf/proto"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

type Releases struct {
	dir string
}

func NewReleases(dir string) *Releases {
	return &Releases{dir: dir}
}

func (r *Releases) Current() (canonical, signature []byte, err error) {
	if r == nil || r.dir == "" {
		return nil, nil, nil
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
	return canonical, signature, nil
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
