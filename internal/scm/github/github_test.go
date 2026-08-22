package github

import "testing"

func TestDirectMainRemoteOnlyMatchesFancive(t *testing.T) {
	for remote, want := range map[string]bool{
		"git@github.com:fancive/repo.git":     true,
		"https://github.com/FANCIVE/repo":     true,
		"git@github.com:kunchenguid/repo.git": false,
		"https://github.com/other/repo.git":   false,
	} {
		if got := DirectMainRemote(remote); got != want {
			t.Errorf("DirectMainRemote(%q) = %v, want %v", remote, got, want)
		}
	}
}

func TestSameRepositoryAcrossURLForms(t *testing.T) {
	if !SameRepository("git@github.com:Fancive/Repo.git", "https://github.com/fancive/repo") {
		t.Fatal("same repository was not recognized")
	}
	if SameRepository("git@github.com:fancive/one.git", "git@github.com:fancive/two.git") {
		t.Fatal("different repositories matched")
	}
}

func TestForkOf(t *testing.T) {
	tests := []struct {
		name     string
		fork     string
		upstream string
		want     bool
	}{
		{
			name:     "common github fork layout",
			fork:     "git@github.com:fancive/prometheus.git",
			upstream: "https://github.com/prometheus/prometheus.git",
			want:     true,
		},
		{
			name:     "same repository",
			fork:     "git@github.com:fancive/prometheus.git",
			upstream: "https://github.com/fancive/prometheus.git",
		},
		{
			name:     "different repository name",
			fork:     "git@github.com:fancive/prometheus.git",
			upstream: "https://github.com/prometheus/node_exporter.git",
		},
		{
			name:     "invalid upstream",
			fork:     "git@github.com:fancive/prometheus.git",
			upstream: "not-a-remote",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ForkOf(test.fork, test.upstream); got != test.want {
				t.Fatalf("ForkOf(%q, %q) = %v, want %v", test.fork, test.upstream, got, test.want)
			}
		})
	}
}

func TestNetworkEndpointUsesGitHubSSHOver443OnlyForDefaultSSH(t *testing.T) {
	tests := map[string]string{
		"git@github.com:fancive/no-mistakes.git":               "ssh://git@ssh.github.com:443/fancive/no-mistakes.git",
		"ssh://git@github.com/fancive/no-mistakes.git":         "ssh://git@ssh.github.com:443/fancive/no-mistakes.git",
		"ssh://git@github.com:22/fancive/no-mistakes.git":      "ssh://git@ssh.github.com:443/fancive/no-mistakes.git",
		"ssh://git@github.com:2222/fancive/no-mistakes.git":    "ssh://git@github.com:2222/fancive/no-mistakes.git",
		"ssh://git@ssh.github.com:443/fancive/no-mistakes.git": "ssh://git@ssh.github.com:443/fancive/no-mistakes.git",
		"https://github.com/fancive/no-mistakes.git":           "https://github.com/fancive/no-mistakes.git",
	}
	for remote, want := range tests {
		if got := NetworkEndpoint(remote); got != want {
			t.Errorf("NetworkEndpoint(%q) = %q, want %q", remote, got, want)
		}
	}
}
