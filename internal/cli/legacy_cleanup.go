package cli

import (
	"context"

	"github.com/kunchenguid/no-mistakes/internal/legacycleanup"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

type legacyCleanupBackend interface {
	Plan(context.Context) (legacycleanup.Plan, error)
	Confirm(context.Context, string) (legacycleanup.Receipt, error)
}

// runLegacyCleanup adapts the migration service to the versioned lean result.
func runLegacyCleanup(ctx context.Context, backend legacyCleanupBackend, request types.LegacyCleanupRequest) (types.GuardResult, error) {
	if request.Plan {
		plan, err := backend.Plan(ctx)
		if err != nil {
			return types.GuardResult{}, err
		}
		result := types.GuardResult{Status: types.GuardPassed, PlanHash: plan.Hash}
		for _, target := range plan.Targets {
			result.CleanupTargets = append(result.CleanupTargets, target.Kind+":"+target.Display)
		}
		if len(plan.Blockers) > 0 {
			result.Status = types.GuardBlocked
			result.Blockers = append(result.Blockers, plan.Blockers...)
		}
		return result, nil
	}
	receipt, err := backend.Confirm(ctx, request.ConfirmHash)
	if err != nil {
		return types.GuardResult{
			Status: types.GuardBlocked, PlanHash: receipt.PlanHash,
			CleanupTargets: append([]string(nil), receipt.Removed...),
		}, err
	}
	return types.GuardResult{
		Status: types.GuardPassed, PlanHash: receipt.PlanHash,
		CleanupTargets: append([]string(nil), receipt.Removed...),
	}, nil
}
