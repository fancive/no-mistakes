package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/legacycleanup"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

type partialCleanupBackend struct{}

func (partialCleanupBackend) Plan(context.Context) (legacycleanup.Plan, error) {
	return legacycleanup.Plan{}, nil
}

func (partialCleanupBackend) Confirm(context.Context, string) (legacycleanup.Receipt, error) {
	return legacycleanup.Receipt{PlanHash: "hash", Removed: []string{"worktree:/owned/run"}}, errors.New("later target changed")
}

func TestLegacyCleanupPreservesPartialReceiptOnFailure(t *testing.T) {
	result, err := runLegacyCleanup(context.Background(), partialCleanupBackend{}, types.LegacyCleanupRequest{ConfirmHash: "hash"})
	if err == nil || result.Status != types.GuardBlocked || result.PlanHash != "hash" || len(result.CleanupTargets) != 1 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}
