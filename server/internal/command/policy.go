package command

import "context"

type Policy interface {
	Authorize(context.Context, Command) error
	Completed(context.Context, Command, error)
}

type policyKey struct{}

func WithPolicy(ctx context.Context, policy Policy) context.Context {
	return context.WithValue(ctx, policyKey{}, policy)
}

func (r *Registry) Authorize(ctx context.Context, name string) error {
	cmd, ok := r.Lookup(name)
	if !ok {
		return nil
	}
	if policy, ok := ctx.Value(policyKey{}).(Policy); ok {
		return policy.Authorize(ctx, cmd)
	}
	return nil
}
