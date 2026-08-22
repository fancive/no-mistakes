//go:build !darwin && !linux

package legacycleanup

import "context"

func removeLegacyService(context.Context, string) error { return nil }
