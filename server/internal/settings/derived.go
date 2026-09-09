package settings

import (
	"github.com/laminara/laminara/server/internal/config"
)

func (d *Doc) derived(path string) string {
	d.once.Do(func() {
		d.filled = config.Derived(d.path)
	})
	return d.filled[path]
}
