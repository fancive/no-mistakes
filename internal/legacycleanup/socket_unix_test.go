//go:build !windows

package legacycleanup

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanBlocksAResponsiveLegacySocket(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "nm-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	socketPath := filepath.Join(root, "socket")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	plan, err := New(Options{Root: root, Reader: fakeStateReader{}, ProcessAlive: func(int) bool { return false }}).Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.Blockers, "\n"), "accepting connections") {
		t.Fatalf("blockers = %v", plan.Blockers)
	}
}
