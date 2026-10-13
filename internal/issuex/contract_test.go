package issuex

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// idleCloser is a RoundTripper that counts CloseIdleConnections calls, as
// http.Client.CloseIdleConnections makes them on its Transport.
type idleCloser struct{ closed atomic.Int32 }

func (c *idleCloser) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("no requests expected")
}
func (c *idleCloser) CloseIdleConnections() { c.closed.Add(1) }

// 02-REQ-1.4, 03-REQ-1.5: Close closes the idle connections of the transport it
// was given.
func TestClose_ClosesIdleTransportConnections(t *testing.T) {
	for name, build := range map[string]func(*http.Client) (Client, error){
		"github": func(hc *http.Client) (Client, error) { return NewGitHub(Options{Token: "t", HTTPClient: hc}) },
		"gitlab": func(hc *http.Client) (Client, error) { return NewGitLab(Options{Token: "t", HTTPClient: hc}) },
	} {
		t.Run(name, func(t *testing.T) {
			spy := &idleCloser{}
			c, err := build(&http.Client{Transport: spy})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Close(); err != nil {
				t.Fatalf("Close() = %v, want nil", err)
			}
			if got := spy.closed.Load(); got != 1 {
				t.Errorf("CloseIdleConnections was called %d times, want 1", got)
			}
		})
	}
}

// 01-REQ-4.2..4.6: the forge NewWithOptions resolves is the one the input names,
// not merely some client: GitHub and GitLab URLs, environment variables and the
// origin remote each yield the matching adapter.
func TestNewWithOptions_ResolvesTheMatchingAdapter(t *testing.T) {
	clear := func(t *testing.T) {
		for _, v := range []string{"GITHUB_API_URL", "GITHUB_TOKEN", "GH_TOKEN", "GITLAB_API_URL", "GITLAB_TOKEN"} {
			t.Setenv(v, "")
		}
	}
	typeOf := func(c Client) string { return fmt.Sprintf("%T", c) }

	t.Run("explicit base URLs", func(t *testing.T) {
		clear(t)
		for opts, want := range map[string]string{
			"https://api.github.com":            "*issuex.githubClient",
			"https://gitlab.example.com/api/v4": "*issuex.gitlabClient",
		} {
			c, err := NewWithOptions(Options{BaseURL: opts})
			if err != nil {
				t.Fatalf("%s: %v", opts, err)
			}
			if got := typeOf(c); got != want {
				t.Errorf("BaseURL %s resolved to %s, want %s", opts, got, want)
			}
			ft, derr := DetectForge(Options{BaseURL: opts})
			if derr != nil || (ft == ForgeTypeGitHub) != strings.Contains(want, "github") {
				t.Errorf("DetectForge(%s) = %v, %v", opts, ft, derr)
			}
		}
	})

	t.Run("environment", func(t *testing.T) {
		for _, tc := range []struct{ env, val, want string }{
			{"GITHUB_TOKEN", "x", "*issuex.githubClient"},
			{"GH_TOKEN", "x", "*issuex.githubClient"},
			{"GITLAB_TOKEN", "x", "*issuex.gitlabClient"},
		} {
			clear(t)
			t.Setenv(tc.env, tc.val)
			dir := t.TempDir()
			old, _ := os.Getwd()
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}
			c, err := NewWithOptions(Options{})
			_ = os.Chdir(old)
			if err != nil {
				t.Fatalf("%s: %v", tc.env, err)
			}
			if got := typeOf(c); got != tc.want {
				t.Errorf("%s resolved to %s, want %s", tc.env, got, tc.want)
			}
		}
	})

	t.Run("default endpoints", func(t *testing.T) {
		clear(t)
		gh, err := NewGitHub(Options{Token: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if got := gh.(*githubClient).baseURL; got != "https://api.github.com" {
			t.Errorf("GitHub default base URL = %q", got)
		}
		gl, err := NewGitLab(Options{Token: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if got := gl.(*gitlabClient).baseURL; got != "https://gitlab.com/api/v4" {
			t.Errorf("GitLab default base URL = %q", got)
		}
	})
}
