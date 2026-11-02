package providers

import (
	"context"
	"encoding/json"
	"os"
)

func (p *sqlProvider) Readiness(ctx context.Context) error { return p.db.PingContext(ctx) }

func (p *jsonFileProvider) Readiness(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := os.ReadFile(p.path)
	if err != nil {
		return err
	}
	var records []map[string]any
	return json.Unmarshal(data, &records)
}
