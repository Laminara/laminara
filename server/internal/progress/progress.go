package progress

import "context"

type Event struct {
	Phase   string
	Current int64
	Total   int64
	Message string
}

type Reporter interface {
	Report(Event)
}

type reporterKey struct{}

func With(ctx context.Context, reporter Reporter) context.Context {
	return context.WithValue(ctx, reporterKey{}, reporter)
}

func Report(ctx context.Context, event Event) {
	if reporter, ok := ctx.Value(reporterKey{}).(Reporter); ok && reporter != nil {
		reporter.Report(event)
	}
}

func Phase(ctx context.Context, phase string) {
	Report(ctx, Event{Phase: phase})
}
