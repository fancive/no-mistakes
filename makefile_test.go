package main

import (
	"os"
	"strings"
	"testing"
)

func TestMakefileBuildsAndInstallsOnlyTheStatelessBinary(t *testing.T) {
	data, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"go build -buildvcs=false -ldflags",
		"install -m 755 bin/no-mistakes",
		"go test -race ./...",
		"go test ./cmd/no-mistakes ./internal/guard ./internal/delivery ./internal/legacycleanup",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Makefile missing %q", want)
		}
	}
	for _, forbidden := range []string{"daemon start", "daemon stop", "TelemetryHost", "recordfixture", "scripts/e2e.sh"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("Makefile retains removed behavior %q", forbidden)
		}
	}
}
