package cli

import (
	"context"

	"github.com/kunchenguid/no-mistakes/internal/delivery"
	"github.com/kunchenguid/no-mistakes/internal/guard"
	"github.com/kunchenguid/no-mistakes/internal/legacycleanup"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

type leanRuntime struct {
	guard    *guard.Service
	delivery *delivery.Service
	cleanup  *legacycleanup.Service
}

func newLeanRuntime(dir string) *leanRuntime {
	return &leanRuntime{
		guard:    guard.New(guard.Options{Dir: dir}),
		delivery: delivery.New(delivery.Options{Dir: dir}),
		cleanup:  legacycleanup.New(legacycleanup.Options{}),
	}
}

func (r *leanRuntime) Check(ctx context.Context, request types.CheckRequest) (types.GuardResult, error) {
	return r.guard.Check(ctx, request)
}

func (r *leanRuntime) Commit(ctx context.Context, request types.CommitRequest) (types.GuardResult, error) {
	return r.guard.Commit(ctx, request)
}

func (r *leanRuntime) Push(ctx context.Context, request types.PushRequest) (types.GuardResult, error) {
	return r.delivery.Push(ctx, request)
}

func (r *leanRuntime) LegacyCleanup(ctx context.Context, request types.LegacyCleanupRequest) (types.GuardResult, error) {
	return runLegacyCleanup(ctx, r.cleanup, request)
}
