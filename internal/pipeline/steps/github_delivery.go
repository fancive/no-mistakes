package steps

import (
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/pipeline"
	"github.com/kunchenguid/no-mistakes/internal/scm"
	scmgithub "github.com/kunchenguid/no-mistakes/internal/scm/github"
)

// isGitHubDirectMainRepo is this fork's delivery-policy boundary. Only a
// fancive-owned upstream with no separate fork target bypasses PR delivery.
func isGitHubDirectMainRepo(sctx *pipeline.StepContext) bool {
	if sctx == nil || sctx.Repo == nil || strings.TrimSpace(sctx.Repo.ForkURL) != "" {
		return false
	}
	return scm.DetectProviderContext(sctx.Ctx, sctx.Repo.UpstreamURL) == scm.ProviderGitHub &&
		scmgithub.DirectMainRemote(sctx.Repo.UpstreamURL)
}
