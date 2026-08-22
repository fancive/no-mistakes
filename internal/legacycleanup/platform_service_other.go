//go:build !windows

package legacycleanup

import "context"

func discoverLegacyPlatformServices(context.Context, string) ([]Target, []string) {
	return nil, nil
}

func removeLegacyPlatformService(context.Context, Target) error { return nil }
