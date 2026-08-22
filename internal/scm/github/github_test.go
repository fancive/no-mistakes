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
