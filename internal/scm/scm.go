// Package scm contains the provider-neutral types retained by lean delivery.
package scm

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/kunchenguid/no-mistakes/internal/shellenv"
)

// Provider is intentionally limited to the two supported delivery contracts.
type Provider string

const (
	ProviderGitHub  Provider = "github"
	ProviderICode   Provider = "icode"
	ProviderUnknown Provider = "unknown"
)

// DetectProviderContext resolves direct hosts and SSH HostName aliases.
func DetectProviderContext(ctx context.Context, remote string) Provider {
	if provider := detectHost(remote); provider != ProviderUnknown {
		return provider
	}
	if !isSSHRemote(remote) {
		return ProviderUnknown
	}
	host := extractHost(remote)
	if host == "" {
		return ProviderUnknown
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(lookupCtx, "ssh", "-G", "--", host)
	shellenv.ConfigureShellCommand(cmd)
	out, err := shellenv.OutputShellCommand(cmd)
	if err != nil {
		return ProviderUnknown
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "hostname") {
			return detectHost(fields[1])
		}
	}
	return ProviderUnknown
}

func detectHost(value string) Provider {
	host := extractHost(value)
	if host == "" {
		host = strings.ToLower(strings.TrimSpace(value))
	}
	switch strings.TrimSuffix(host, ".") {
	case "github.com":
		return ProviderGitHub
	case "icode.baidu.com":
		return ProviderICode
	default:
		return ProviderUnknown
	}
}

func extractHost(remote string) string {
	value := strings.TrimSpace(remote)
	if marker := strings.Index(value, "://"); marker >= 0 {
		value = value[marker+3:]
		if slash := strings.IndexByte(value, '/'); slash >= 0 {
			value = value[:slash]
		}
		if at := strings.LastIndexByte(value, '@'); at >= 0 {
			value = value[at+1:]
		}
		if colon := strings.LastIndexByte(value, ':'); colon >= 0 {
			value = value[:colon]
		}
		return strings.ToLower(value)
	}
	if colon := strings.IndexByte(value, ':'); colon >= 0 {
		value = value[:colon]
	}
	if at := strings.LastIndexByte(value, '@'); at >= 0 {
		value = value[at+1:]
	}
	return strings.ToLower(value)
}

func isSSHRemote(remote string) bool {
	lower := strings.ToLower(strings.TrimSpace(remote))
	return strings.HasPrefix(lower, "ssh://") || !strings.Contains(lower, "://") && strings.Contains(lower, ":")
}

// PR identifies one iCode review.
type PR struct {
	Number string
	URL    string
}

type PRState string

const (
	PRStateOpen   PRState = "OPEN"
	PRStateMerged PRState = "MERGED"
	PRStateClosed PRState = "CLOSED"
)

type CheckBucket string

const (
	CheckBucketPass    CheckBucket = "pass"
	CheckBucketFail    CheckBucket = "fail"
	CheckBucketPending CheckBucket = "pending"
	CheckBucketCancel  CheckBucket = "cancel"
	CheckBucketSkip    CheckBucket = "skipping"
)

// Check is the normalized iCode machine-check record used by delivery.
type Check struct {
	Name   string
	Bucket CheckBucket
	State  string
	Link   string
}

// ReviewSubmission is the result of the iCode approval/submit attempt.
type ReviewSubmission struct {
	Submitted bool
	Pending   bool
	Message   string
}
