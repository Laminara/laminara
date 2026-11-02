package hwid

import "context"

func (s *SQLStore) Readiness(ctx context.Context) error { return s.db.PingContext(ctx) }

func (g *Gate) Readiness(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if store, ok := g.Store().(interface{ Readiness(context.Context) error }); ok {
		return store.Readiness(ctx)
	}
	return nil
}
