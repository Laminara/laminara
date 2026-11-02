package buildsvc

import (
	"context"
	"errors"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/progress"
	"io"
	"testing"
)

type cancelOnSigning struct{ cancel context.CancelFunc }

func (r cancelOnSigning) Report(event progress.Event) {
	if event.Phase == "Подпись манифеста" {
		r.cancel()
	}
}

func TestCancelledPublicationPreservesCurrent(t *testing.T) {
	service, _ := syncFixture(t)
	if err := service.prepare(t.Context(), []string{"pack", "1.21.1", "platform=linux"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := service.publish(t.Context(), []string{"pack"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	first, err := catalog.ReadRelease(service.profilesDir, "pack")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx = progress.With(ctx, cancelOnSigning{cancel: cancel})
	publishErr := service.publish(ctx, []string{"pack"}, io.Discard)
	current, readErr := catalog.ReadRelease(service.profilesDir, "pack")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !errors.Is(publishErr, context.Canceled) || current.Current != first.Current {
		t.Fatalf("cancelled publish: error=%v, context=%v, current changed=%v", publishErr, ctx.Err(), current.Current != first.Current)
	}
}
