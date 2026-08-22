package skill

import (
	"strings"
	"testing"
)

func TestMarkdownDescribesOnlyLeanGuard(t *testing.T) {
	md := Markdown()
	for _, want := range []string{
		"name: " + Name,
		"user-invocable: true",
		"synchronous, stateless shipping guard",
		"no-mistakes check",
		"no-mistakes commit",
		"no-mistakes push",
		"no-mistakes legacy-cleanup --plan",
		"NO_MISTAKES_CHANGED_FILES_FILE",
		"$ipipe-pull-branch",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("generated skill missing %q", want)
		}
	}
	for _, forbidden := range []string{"no-mistakes axi run", "runs an AI reviewer", "spins up a disposable worktree"} {
		if strings.Contains(md, forbidden) {
			t.Errorf("generated skill retains removed behavior %q", forbidden)
		}
	}
}
