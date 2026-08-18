package icode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/scm"
)

func TestRepoPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "ssh URL", url: "ssh://user@icode.baidu.com:8235/baidu/inputmethod/v5api", want: "baidu/inputmethod/v5api"},
		{name: "scp SSH", url: "git@icode.baidu.com:baidu/inputmethod/v5api.git", want: "baidu/inputmethod/v5api"},
		{name: "HTTPS", url: "https://icode.baidu.com/baidu/inputmethod/v5api.git", want: "baidu/inputmethod/v5api"},
		{name: "host only", url: "https://icode.baidu.com", want: ""},
		{name: "empty", url: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := RepoPath(tt.url); got != tt.want {
				t.Fatalf("RepoPath(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestFindPRMatchesCurrentRevision(t *testing.T) {
	t.Parallel()

	host := New(testCmdFactory(map[string]testResponse{
		"icode-cli api get_repo_reviews --repo baidu/inputmethod/v5api --status NEW -o json": {
			stdout: `{"status":"OK","data":{"changes":[` +
				`{"_number":122,"branch":"release","current_revision":"other"},` +
				`{"_number":123,"branch":"release","current_revision":"abc123"}` +
				`]}}`,
		},
	}), func() bool { return true }, "baidu/inputmethod/v5api", "abc123")

	pr, err := host.FindPR(context.Background(), "release", "release")
	if err != nil {
		t.Fatalf("FindPR() error = %v", err)
	}
	if pr == nil || pr.Number != "123" {
		t.Fatalf("FindPR() = %+v, want review 123", pr)
	}
	wantURL := "https://console.cloud.baidu-int.com/devops/icode/repos/baidu/inputmethod/v5api/reviews/123/"
	if pr.URL != wantURL {
		t.Fatalf("FindPR() URL = %q, want %q", pr.URL, wantURL)
	}
}

func TestGetPRStateNormalizesICodeStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   scm.PRState
	}{
		{status: "NEW", want: scm.PRStateOpen},
		{status: "MERGED", want: scm.PRStateMerged},
		{status: "ABANDONED", want: scm.PRStateClosed},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			host := New(testCmdFactory(map[string]testResponse{
				"icode-cli api get_review_info --change-number 123 -o json": {
					stdout: fmt.Sprintf(`{"status":"OK","data":{"_number":123,"status":%q}}`, tt.status),
				},
			}), func() bool { return true }, "baidu/inputmethod/v5api", "abc123")

			got, err := host.GetPRState(context.Background(), &scm.PR{Number: "123"})
			if err != nil {
				t.Fatalf("GetPRState() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("GetPRState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetChecksCombinesMachineChecksAndPipelines(t *testing.T) {
	t.Parallel()

	host := New(testCmdFactory(map[string]testResponse{
		"icode-cli api get_machine_check --change-number 123 -o json": {
			stdout: `{"status":"OK","data":{` +
				`"style":{"operations":[` +
				`{"name":"CodeStyle","status":"SUCCESS"},` +
				`{"name":"BugBye","status":"FAILED"}` +
				`]},` +
				`"pipeline":{"hasPipelines":true,"status":"RUNNING","changePipelines":[` +
				`{"pipelineName":"ChangePipeline","result":"RUNNING","pipelineUrl":"https://ipipe/run/1"}` +
				`]}` +
				`}}`,
		},
	}), func() bool { return true }, "baidu/inputmethod/v5api", "abc123")

	checks, err := host.GetChecks(context.Background(), &scm.PR{Number: "123"})
	if err != nil {
		t.Fatalf("GetChecks() error = %v", err)
	}
	if len(checks) != 3 {
		t.Fatalf("len(checks) = %d, want 3: %+v", len(checks), checks)
	}
	wants := map[string]scm.CheckBucket{
		"CodeStyle":      scm.CheckBucketPass,
		"BugBye":         scm.CheckBucketFail,
		"ChangePipeline": scm.CheckBucketPending,
	}
	for _, check := range checks {
		if want, ok := wants[check.Name]; !ok || check.Bucket != want {
			t.Fatalf("unexpected check %+v; wants=%v", check, wants)
		}
		if check.Name == "ChangePipeline" && check.Link != "https://ipipe/run/1" {
			t.Fatalf("pipeline link = %q", check.Link)
		}
	}
}

func TestGetChecksKeepsEmptyPipelineRegistrationPending(t *testing.T) {
	t.Parallel()

	host := New(testCmdFactory(map[string]testResponse{
		"icode-cli api get_machine_check --change-number 123 -o json": {
			stdout: `{"status":"OK","data":{"style":{"operations":[]},"pipeline":{"hasPipelines":true,"status":"RUNNING","changePipelines":[]}}}`,
		},
	}), func() bool { return true }, "baidu/inputmethod/v5api", "abc123")

	checks, err := host.GetChecks(context.Background(), &scm.PR{Number: "123"})
	if err != nil {
		t.Fatalf("GetChecks() error = %v", err)
	}
	if len(checks) != 1 || checks[0].Name != "iPipe" || checks[0].Bucket != scm.CheckBucketPending {
		t.Fatalf("GetChecks() = %+v, want pending synthetic iPipe check", checks)
	}
}

func TestAvailableRequiresCLIAndAuthenticatedAPI(t *testing.T) {
	t.Parallel()

	host := New(nil, func() bool { return false }, "baidu/inputmethod/v5api", "")
	if err := host.Available(context.Background()); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("Available() error = %v, want missing CLI", err)
	}

	host = New(testCmdFactory(map[string]testResponse{
		"icode-cli api get_submit_settings --repo baidu/inputmethod/v5api --check-permission -o json": {
			stdout: `{"status":"OK","data":{}}`,
		},
	}), func() bool { return true }, "baidu/inputmethod/v5api", "")
	if err := host.Available(context.Background()); err != nil {
		t.Fatalf("Available() error = %v", err)
	}
}

func TestEnsureSubmittedSelfScoresAndSubmits(t *testing.T) {
	t.Parallel()

	host := New(testCmdFactory(map[string]testResponse{
		"icode-cli api get_review_info --change-number 123 -o json": {
			stdout: `{"status":"OK","data":{"_number":123,"status":"NEW","labels":{"Code-Review":{"all":[{"value":0}]}}}}`,
		},
		"icode-cli api set_review_score --repo baidu/inputmethod/v5api --change-number 123 --score 2 -o json": {
			stdout: `{"status":"OK","data":{}}`,
		},
		"icode-cli api submit_review --repo baidu/inputmethod/v5api --change-number 123 -o json": {
			stdout: `{"status":"OK","data":{}}`,
		},
	}), func() bool { return true }, "baidu/inputmethod/v5api", "abc123", Options{Reviewers: "reviewer1,reviewer2", AutoSubmit: true})

	result, err := host.EnsureSubmitted(context.Background(), &scm.PR{Number: "123"})
	if err != nil {
		t.Fatalf("EnsureSubmitted() error = %v", err)
	}
	if !result.Submitted || result.Pending {
		t.Fatalf("EnsureSubmitted() = %+v, want submitted", result)
	}
}

func TestEnsureSubmittedAddsReviewersAndWaitsForExternalPlus2(t *testing.T) {
	t.Parallel()

	host := New(testCmdFactory(map[string]testResponse{
		"icode-cli api get_review_info --change-number 123 -o json": {
			stdout: `{"status":"OK","data":{"_number":123,"status":"NEW","labels":{"Code-Review":{"all":[{"value":0}]}}}}`,
		},
		"icode-cli api set_review_score --repo baidu/inputmethod/v5api --change-number 123 --score 2 -o json": {
			stdout: `{"status":"ERROR","message":"无合入权限"}`,
		},
		"icode-cli api add_reviewers --change-number 123 --reviewers reviewer1,reviewer2 -o json": {
			stdout: `{"status":"OK","data":{}}`,
		},
		"icode-cli api submit_review --repo baidu/inputmethod/v5api --change-number 123 -o json": {
			stdout: `{"status":"ERROR","message":"未通过评审，需要 +2"}`,
		},
	}), func() bool { return true }, "baidu/inputmethod/v5api", "abc123", Options{Reviewers: "reviewer1,reviewer2", AutoSubmit: true})

	result, err := host.EnsureSubmitted(context.Background(), &scm.PR{Number: "123"})
	if err != nil {
		t.Fatalf("EnsureSubmitted() error = %v", err)
	}
	if !result.Pending || result.Submitted {
		t.Fatalf("EnsureSubmitted() = %+v, want pending external +2", result)
	}
}

func TestEnsureSubmittedDoesNotWriteWithoutTrustedOptIn(t *testing.T) {
	t.Parallel()

	host := New(testCmdFactory(nil), func() bool { return true }, "baidu/inputmethod/v5api", "abc123")
	result, err := host.EnsureSubmitted(context.Background(), &scm.PR{Number: "123"})
	if err != nil {
		t.Fatalf("EnsureSubmitted() error = %v", err)
	}
	if !result.Pending || result.Submitted || !strings.Contains(result.Message, "disabled") {
		t.Fatalf("EnsureSubmitted() = %+v, want disabled pending state", result)
	}
}

type testResponse struct {
	stdout string
	stderr string
	code   int
}

func testCmdFactory(responses map[string]testResponse) CmdFactory {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		key := strings.TrimSpace(name + " " + strings.Join(args, " "))
		response, ok := responses[key]
		if !ok {
			response = testResponse{stderr: "unexpected command: " + key, code: 1}
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestICodeHelperProcess", "--", key)
		cmd.Env = append(os.Environ(),
			"ICODE_TEST_HELPER=1",
			"ICODE_TEST_STDOUT="+response.stdout,
			"ICODE_TEST_STDERR="+response.stderr,
			fmt.Sprintf("ICODE_TEST_EXIT_CODE=%d", response.code),
		)
		return cmd
	}
}

func TestICodeHelperProcess(t *testing.T) {
	if os.Getenv("ICODE_TEST_HELPER") != "1" {
		return
	}
	_, _ = fmt.Fprint(os.Stdout, os.Getenv("ICODE_TEST_STDOUT"))
	_, _ = fmt.Fprint(os.Stderr, os.Getenv("ICODE_TEST_STDERR"))
	if os.Getenv("ICODE_TEST_EXIT_CODE") != "" && os.Getenv("ICODE_TEST_EXIT_CODE") != "0" {
		os.Exit(1)
	}
	os.Exit(0)
}
