package update

import (
	"os"
	"testing"

	"github.com/bestruirui/octopus/internal/conf"
)

func TestNormalizeGitHubRepo(t *testing.T) {
	cases := map[string]string{
		"SuperJeason/octopus":                       "SuperJeason/octopus",
		"https://github.com/SuperJeason/octopus":    "SuperJeason/octopus",
		"https://github.com/SuperJeason/octopus.git": "SuperJeason/octopus",
		"github.com/SuperJeason/octopus/":           "SuperJeason/octopus",
	}
	for in, want := range cases {
		if got := normalizeGitHubRepo(in); got != want {
			t.Fatalf("normalizeGitHubRepo(%q)=%q want %q", in, got, want)
		}
	}
}

func TestResolveUpdateRepoPriority(t *testing.T) {
	oldRepo := conf.Repo
	t.Cleanup(func() {
		conf.Repo = oldRepo
		_ = os.Unsetenv("OCTOPUS_UPDATE_REPO")
	})

	conf.Repo = "https://github.com/SuperJeason/octopus"
	_ = os.Unsetenv("OCTOPUS_UPDATE_REPO")
	if got := resolveUpdateRepo(); got != "SuperJeason/octopus" {
		t.Fatalf("from conf.Repo: got %q", got)
	}

	if err := os.Setenv("OCTOPUS_UPDATE_REPO", "other/fork"); err != nil {
		t.Fatal(err)
	}
	if got := resolveUpdateRepo(); got != "other/fork" {
		t.Fatalf("env should win: got %q", got)
	}
}
