package issuex

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNewWithOptions_NoOpIsTheOfflineClient(t *testing.T) {
	c, err := NewWithOptions(Options{NoOp: true, BaseURL: "https://api.github.com", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.(*NoOpClient); !ok || c.Authenticated() {
		t.Errorf("NewWithOptions(NoOp) = %T (authenticated %v), want the unauthenticated NoOpClient", c, c.Authenticated())
	}
}

// The forge NewWithOptions resolves is the one the input names, and its
// client carries the resolved repository and credential.
func TestNewWithOptions_BuildsTheResolvedForgesClient(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      map[string]string
		opts     Options
		want     string
		wantRepo Repo
		wantAuth bool
	}{
		{"GitHub base URL", nil, Options{BaseURL: "https://api.github.com"}, "*issuex.githubClient", Repo{}, false},
		{"GitLab base URL", nil, Options{BaseURL: "https://gitlab.example.com/api/v4"}, "*issuex.gitlabClient", Repo{}, false},
		{"GITHUB_TOKEN", map[string]string{"GITHUB_TOKEN": "x"}, Options{}, "*issuex.githubClient", Repo{}, true},
		{"GH_TOKEN", map[string]string{"GH_TOKEN": "x"}, Options{}, "*issuex.githubClient", Repo{}, true},
		{"GITLAB_TOKEN", map[string]string{"GITLAB_TOKEN": "x"}, Options{}, "*issuex.gitlabClient", Repo{}, true},
		{"GitHub remote", nil,
			Options{Repo: Repo{Owner: "agentfox", Name: "agent-fox"}, RemoteURL: "https://github.com/agentfox/agent-fox.git", Token: "dummy-token"},
			"*issuex.githubClient", Repo{Owner: "agentfox", Name: "agent-fox"}, true},
		{"GitLab remote", nil,
			Options{RemoteURL: "https://gitlab.com/group/subgroup/project.git", Token: "glpat-token"},
			"*issuex.gitlabClient", Repo{Host: "gitlab.com", Owner: "group/subgroup", Name: "project"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			c, err := NewWithOptions(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			var repo Repo
			switch c := c.(type) {
			case *githubClient:
				repo = c.repo
			case *gitlabClient:
				repo = c.repo
			}
			if got := fmt.Sprintf("%T", c); got != tc.want || repo != tc.wantRepo || c.Authenticated() != tc.wantAuth {
				t.Errorf("NewWithOptions = %s for %+v (authenticated %v), want %s for %+v (%v)",
					got, repo, c.Authenticated(), tc.want, tc.wantRepo, tc.wantAuth)
			}
		})
	}
}

// The registry is how every forge reaches its adapter, GitHub and GitLab
// included: the client is the registered adapter's, built from the resolved
// options.
func TestNewWithOptions_BuildsThroughTheRegisteredAdapter(t *testing.T) {
	clearForgeEnv(t)
	spy := NewNoOp()
	var got Options
	withAdapter(t, ForgeTypeGitHub, func(o Options) (Client, error) {
		got = o
		return spy, nil
	})

	client, err := NewWithOptions(Options{BaseURL: "https://api.github.com", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if client != spy {
		t.Errorf("NewWithOptions returned %T, want the registered adapter's client", client)
	}
	if got.BaseURL != "https://api.github.com" || got.Token != "t" || got.UserAgent != "spec" {
		t.Errorf("the adapter received %+v, want the resolved options with the default user agent", got)
	}
}

// A forge that resolves while its adapter is not registered is
// ErrUnsupportedForge, with no client.
func TestNewWithOptions_RefusesAForgeWithNoAdapter(t *testing.T) {
	for _, tc := range []struct {
		forge   ForgeType
		baseURL string
	}{
		{ForgeTypeGitHub, "https://api.github.com"},
		{ForgeTypeGitLab, "https://gitlab.example.com/api/v4"},
	} {
		t.Run(string(tc.forge), func(t *testing.T) {
			clearForgeEnv(t)
			withAdapter(t, tc.forge, nil)

			client, err := NewWithOptions(Options{BaseURL: tc.baseURL, Token: "fake"})
			if client != nil || !errors.Is(err, ErrUnsupportedForge) {
				t.Errorf("NewWithOptions = %v, %v; want no client and ErrUnsupportedForge", client, err)
			}
		})
	}
}

// A host whose name says neither github nor gitlab is ErrAmbiguousForge, and
// no request, with or without a credential, leaves for it. The server
// answers like a GitLab would, so a probe would succeed.
func TestNewWithOptions_SendsNothingToAnUnclassifiedHost(t *testing.T) {
	var requests atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		t.Errorf("unexpected request %s %s with PRIVATE-TOKEN %q", r.Method, r.URL.Path, r.Header.Get("PRIVATE-TOKEN"))
		_, _ = w.Write([]byte(`{"version": "16.8.0-ee"}`))
	})
	plain := httptest.NewServer(handler)
	defer plain.Close()
	secure := httptest.NewTLSServer(handler)
	defer secure.Close()
	secureHost := strings.TrimPrefix(secure.URL, "https://")
	nonGit := t.TempDir()

	for name, opts := range map[string]Options{
		"BaseURL":   {BaseURL: plain.URL},
		"RemoteURL": {RemoteURL: plain.URL + "/corp/app.git", Repo: Repo{Owner: "corp", Name: "app"}},
		"Repo.Host": {Repo: Repo{Owner: "corp", Name: "app", Host: secureHost}, HTTPClient: secure.Client()},
		"Token":     {BaseURL: plain.URL, Token: "glpat-explicit"},
	} {
		t.Run(name, func(t *testing.T) {
			setEnv(t, map[string]string{"GITLAB_TOKEN": "glpat-SECRET-from-env", "GITHUB_TOKEN": "ghp-SECRET-from-env"})
			opts.Dir = nonGit
			client, err := NewWithOptions(opts)
			if client != nil || !errors.Is(err, ErrAmbiguousForge) {
				t.Errorf("NewWithOptions = %T, %v; want no client and ErrAmbiguousForge", client, err)
			}
		})
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d request(s) reached an unclassified host, want 0", n)
	}
}
