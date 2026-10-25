package issuex

import (
	"errors"
	"testing"
)

// A base URL that names one forge is that forge; a name that happens to
// contain another forge's name too (a repository called bitbucket-mirror)
// does not make it ambiguous, only github together with gitlab does. A base
// URL naming neither (a test server, a GHE domain) is decided by the remote
// or the repository host, or not at all.
func TestDetectForge_FromTheBaseURL(t *testing.T) {
	clearForgeEnv(t)
	for _, tc := range []struct {
		name     string
		opts     Options
		want     ForgeType
		wantRepo Repo
	}{
		{"GitHub API", Options{BaseURL: "https://api.github.com/"}, ForgeTypeGitHub, Repo{}},
		{"GitLab instance", Options{BaseURL: " https://gitlab.example.com/api/v4 "}, ForgeTypeGitLab, Repo{}},
		{"github with another forge's name", Options{BaseURL: "https://github.com/bitbucket-mirror"}, ForgeTypeGitHub, Repo{}},
		{"github API path with another forge's name", Options{BaseURL: "https://api.github.com/repos/bitbucket"}, ForgeTypeGitHub, Repo{}},
		{"neither, decided by a GitHub remote", Options{BaseURL: "http://127.0.0.1:9", RemoteURL: "git@github.com:o/r.git"},
			ForgeTypeGitHub, Repo{Host: "github.com", Owner: "o", Name: "r"}},
		{"neither, decided by a GitLab remote", Options{BaseURL: "http://127.0.0.1:9", RemoteURL: "https://gitlab.com/g/p.git"},
			ForgeTypeGitLab, Repo{Host: "gitlab.com", Owner: "g", Name: "p"}},
		{"neither, a remote does not replace the given repository",
			Options{BaseURL: "http://127.0.0.1:9", RemoteURL: "https://gitlab.com/g/p.git", Repo: Repo{Owner: "mine", Name: "repo"}},
			ForgeTypeGitLab, Repo{Owner: "mine", Name: "repo"}},
		{"neither, decided by a GitHub repository host", Options{BaseURL: "http://127.0.0.1:9", Repo: Repo{Host: "github.com", Owner: "o", Name: "r"}},
			ForgeTypeGitHub, Repo{Host: "github.com", Owner: "o", Name: "r"}},
		{"neither, decided by a GitLab repository host", Options{BaseURL: "http://127.0.0.1:9", Repo: Repo{Host: "gitlab.com", Owner: "g", Name: "p"}},
			ForgeTypeGitLab, Repo{Host: "gitlab.com", Owner: "g", Name: "p"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, opts, err := detectForge(tc.opts)
			if err != nil || got != tc.want {
				t.Fatalf("detectForge = %q, %v; want %q", got, err, tc.want)
			}
			if opts.Repo != tc.wantRepo {
				t.Errorf("resolved Repo = %+v, want %+v", opts.Repo, tc.wantRepo)
			}
			if ft, err := DetectForge(tc.opts); ft != got || err != nil {
				t.Errorf("DetectForge = %q, %v; want what detectForge resolved", ft, err)
			}
		})
	}
}

// A forge that cannot be told is ErrAmbiguousForge: nothing is guessed.
func TestDetectForge_RefusesWhatItCannotTell(t *testing.T) {
	nonGit := t.TempDir()
	for _, tc := range []struct {
		name string
		env  map[string]string
		opts Options
	}{
		{"a base URL naming both forges", nil, Options{BaseURL: "https://gitlab.com/github/repo"}},
		{"a base URL naming neither", nil, Options{BaseURL: "https://forge.example.com"}},
		{"a base URL naming neither, with a remote naming neither", nil,
			Options{BaseURL: "https://forge.example.com", RemoteURL: "https://bitbucket.org/o/r.git", Repo: Repo{Host: "bitbucket.org", Owner: "o", Name: "r"}}},
		{"a remote on another host", map[string]string{"GITHUB_TOKEN": "gh"}, Options{RemoteURL: "https://bitbucket.org/o/r.git"}},
		{"an SCP remote on another host", map[string]string{"GITHUB_TOKEN": "gh"}, Options{RemoteURL: "git@bitbucket.org:o/r.git"}},
		{"a repository on another host", map[string]string{"GITHUB_TOKEN": "gh"}, Options{Repo: Repo{Host: "git.example.com", Owner: "o", Name: "r"}}},
		{"a host both API URLs name", map[string]string{"GITHUB_API_URL": "https://git.example.com", "GITLAB_API_URL": "https://git.example.com/api/v4"},
			Options{Repo: Repo{Host: "git.example.com", Owner: "o", Name: "r"}}},
		{"no configuration and no repository", nil, Options{Dir: nonGit}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			if ft, err := DetectForge(tc.opts); !errors.Is(err, ErrAmbiguousForge) || ft != ForgeTypeUnknown {
				t.Errorf("DetectForge = %q, %v; want ErrAmbiguousForge", ft, err)
			}
		})
	}
}

// A remote on a known forge decides it, and names the repository unless one
// was given.
func TestDetectForge_FromTheRemoteURL(t *testing.T) {
	clearForgeEnv(t)
	for _, tc := range []struct {
		name     string
		opts     Options
		want     ForgeType
		wantBase string
		wantRepo Repo
	}{
		{"GitHub", Options{RemoteURL: "https://github.com/o/r.git"}, ForgeTypeGitHub, "https://api.github.com", Repo{Host: "github.com", Owner: "o", Name: "r"}},
		{"GitLab", Options{RemoteURL: "git@gitlab.com:g/sub/p.git"}, ForgeTypeGitLab, "https://gitlab.com/api/v4", Repo{Host: "gitlab.com", Owner: "g/sub", Name: "p"}},
		{"given repository kept", Options{RemoteURL: "https://github.com/o/r.git", Repo: Repo{Owner: "agentfox", Name: "agent-fox"}},
			ForgeTypeGitHub, "https://api.github.com", Repo{Owner: "agentfox", Name: "agent-fox"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, opts, err := detectForge(tc.opts)
			if err != nil || got != tc.want || opts.BaseURL != tc.wantBase || opts.Repo != tc.wantRepo {
				t.Errorf("detectForge = %q at %q for %+v, %v; want %q at %q for %+v", got, opts.BaseURL, opts.Repo, err, tc.want, tc.wantBase, tc.wantRepo)
			}
		})
	}

	// A remote that is not a remote at all leaves the decision to what comes
	// after it.
	setEnv(t, map[string]string{"GITLAB_TOKEN": "gl"})
	if got, err := DetectForge(Options{RemoteURL: "not-a-remote"}); err != nil || got != ForgeTypeGitLab {
		t.Errorf("DetectForge(unparsable remote) = %q, %v; want gitlab from the environment", got, err)
	}
}

// A credential goes only to the host its configuration names: the host of
// the API URL, or the public forge when no API URL is configured. Any other
// host gets its own API address and no token.
func TestDetectForge_TheTokenGoesOnlyToTheHostItIsConfiguredFor(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       map[string]string
		host      string
		token     string
		wantType  ForgeType
		wantBase  string
		wantToken string
	}{
		{"github.com, no API URL", map[string]string{"GITHUB_TOKEN": "gh"},
			"github.com", "", ForgeTypeGitHub, "https://api.github.com", "gh"},
		{"github.com with its own API URL", map[string]string{"GITHUB_TOKEN": "gh", "GITHUB_API_URL": "https://api.github.com"},
			"github.com", "", ForgeTypeGitHub, "https://api.github.com", "gh"},
		{"github.com with a GHES API URL", map[string]string{"GITHUB_TOKEN": "gh", "GITHUB_API_URL": "https://ghe.acme.com/api/v3"},
			"github.com", "", ForgeTypeGitHub, "https://api.github.com", ""},
		{"github.com without any configuration", nil,
			"github.com", "", ForgeTypeGitHub, "https://api.github.com", ""},
		{"GHES named by its API URL", map[string]string{"GITHUB_TOKEN": "gh", "GITHUB_API_URL": "https://ghe.acme.com/api/v3/"},
			"ghe.acme.com", "", ForgeTypeGitHub, "https://ghe.acme.com/api/v3", "gh"},
		{"self-hosted GitHub, no API URL", map[string]string{"GITHUB_TOKEN": "gh"},
			"github.acme.com", "", ForgeTypeGitHub, "https://github.acme.com/api/v3", ""},
		{"gitlab.com", map[string]string{"GITLAB_TOKEN": "gl"},
			"gitlab.com", "", ForgeTypeGitLab, "https://gitlab.com/api/v4", "gl"},
		{"self-hosted GitLab, no API URL", map[string]string{"GITLAB_TOKEN": "gl"},
			"gitlab.acme.com", "", ForgeTypeGitLab, "https://gitlab.acme.com/api/v4", ""},
		{"self-hosted GitLab named by its API URL", map[string]string{"GITLAB_TOKEN": "gl", "GITLAB_API_URL": "https://git.example.com"},
			"git.example.com", "", ForgeTypeGitLab, "https://git.example.com", "gl"},
		{"an explicit token", map[string]string{"GITLAB_TOKEN": "gl"},
			"gitlab.acme.com", "mine", ForgeTypeGitLab, "https://gitlab.acme.com/api/v4", "mine"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			opts := Options{Token: tc.token, Repo: Repo{Host: tc.host, Owner: "g", Name: "p"}}

			ft, resolved, err := detectForge(opts)
			if err != nil {
				t.Fatalf("detectForge: %v", err)
			}
			if ft != tc.wantType || resolved.BaseURL != tc.wantBase {
				t.Errorf("forge %q at %q, want %q at %q", ft, resolved.BaseURL, tc.wantType, tc.wantBase)
			}
			c, err := NewWithOptions(opts)
			if err != nil {
				t.Fatalf("NewWithOptions: %v", err)
			}
			var token string
			switch c := c.(type) {
			case *githubClient:
				token = c.token
			case *gitlabClient:
				token = c.token
			}
			if token != tc.wantToken || c.Authenticated() != (tc.wantToken != "") {
				t.Errorf("token = %q (Authenticated %v), want %q", token, c.Authenticated(), tc.wantToken)
			}
		})
	}
}

// With only one forge configured in the environment, that forge is used,
// at its configured API URL or the public one.
func TestDetectForge_FromTheEnvironment(t *testing.T) {
	nonGit := t.TempDir()
	for _, tc := range []struct {
		name      string
		env       map[string]string
		opts      Options
		want      ForgeType
		wantBase  string
		wantToken string
	}{
		{"GITHUB_TOKEN", map[string]string{"GITHUB_TOKEN": "gh"}, Options{}, ForgeTypeGitHub, "https://api.github.com", "gh"},
		{"GH_TOKEN", map[string]string{"GH_TOKEN": "gh2"}, Options{}, ForgeTypeGitHub, "https://api.github.com", "gh2"},
		{"GITHUB_API_URL", map[string]string{"GITHUB_API_URL": "https://ghe.acme.com/api/v3"}, Options{}, ForgeTypeGitHub, "https://ghe.acme.com/api/v3", ""},
		{"GITLAB_TOKEN", map[string]string{"GITLAB_TOKEN": "gl"}, Options{}, ForgeTypeGitLab, "https://gitlab.com/api/v4", "gl"},
		{"GITLAB_API_URL", map[string]string{"GITLAB_API_URL": "https://code.acme.com/api/v4"}, Options{}, ForgeTypeGitLab, "https://code.acme.com/api/v4", ""},
		{"an explicit token beats the environment's", map[string]string{"GITLAB_TOKEN": "gl"}, Options{Token: "mine"}, ForgeTypeGitLab, "https://gitlab.com/api/v4", "mine"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			opts := tc.opts
			opts.Dir = nonGit
			got, resolved, err := detectForge(opts)
			if err != nil || got != tc.want || resolved.BaseURL != tc.wantBase || resolved.Token != tc.wantToken {
				t.Errorf("detectForge = %q at %q with token %q, %v; want %q at %q with %q",
					got, resolved.BaseURL, resolved.Token, err, tc.want, tc.wantBase, tc.wantToken)
			}
		})
	}
}

// With both forges configured, or neither, the origin remote decides: of
// Options.Dir, not of wherever the process runs, unless Dir is empty.
func TestDetectForge_FromTheOriginRemote(t *testing.T) {
	gitlabDir := gitRepoWithOrigin(t, "https://gitlab.com/team/app.git")
	githubDir := gitRepoWithOrigin(t, "git@github.com:team/project.git")

	t.Run("both configured", func(t *testing.T) {
		// This test runs in a checkout whose own origin is on GitHub.
		setEnv(t, map[string]string{"GITHUB_TOKEN": "gh", "GITLAB_TOKEN": "gl"})
		ft, opts, err := detectForge(Options{Dir: gitlabDir})
		if err != nil || ft != ForgeTypeGitLab || opts.Token != "gl" {
			t.Errorf("forge = %q with token %q (%v), want gitlab with its token from %s's origin", ft, opts.Token, err, gitlabDir)
		}
	})
	t.Run("neither configured", func(t *testing.T) {
		clearForgeEnv(t)
		ft, opts, err := detectForge(Options{Dir: githubDir})
		if err != nil || ft != ForgeTypeGitHub || opts.BaseURL != "https://api.github.com" {
			t.Errorf("forge = %q at %q (%v), want github", ft, opts.BaseURL, err)
		}
	})
	t.Run("the working directory when no Dir is given", func(t *testing.T) {
		clearForgeEnv(t)
		t.Chdir(gitlabDir)
		c, err := NewWithOptions(Options{})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := c.(*gitlabClient); !ok {
			t.Errorf("NewWithOptions in a GitLab checkout = %T", c)
		}
	})
	t.Run("an origin on a host both API URLs name", func(t *testing.T) {
		setEnv(t, map[string]string{"GITHUB_API_URL": "https://git.example.com", "GITLAB_API_URL": "https://git.example.com/api/v4"})
		dir := gitRepoWithOrigin(t, "https://git.example.com/team/app.git")
		if ft, err := DetectForge(Options{Dir: dir}); !errors.Is(err, ErrAmbiguousForge) {
			t.Errorf("DetectForge = %q, %v; want ErrAmbiguousForge", ft, err)
		}
	})
}
