package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/internal/issuex"
)

// The argument is classified without a model: "-" is stdin, a readable file is
// read, and everything else is text, a directory and a path that does not exist
// included, since "the widget/ package panics" is a plausible report.
func TestResolve_ClassifiesTheArgument(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "crash.log")
	if err := os.WriteFile(file, []byte("panic: nil map\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, arg, stdin string
		kind             SourceKind
		origin, body     string
	}{
		{"text", "the widget counter double-counts on retry", "", KindText, "argument", "the widget counter double-counts on retry"},
		{"file", file, "", KindFile, file, "panic: nil map\n"},
		{"directory", dir, "", KindText, "argument", dir},
		{"missing path", "internal/nope/missing.go panics on empty input", "", KindText, "argument", "internal/nope/missing.go panics on empty input"},
		{"stdin", "-", "piped report\n", KindStdin, "stdin", "piped report\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Resolve(context.Background(), tc.arg, strings.NewReader(tc.stdin), nil, nil)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if in.Kind != tc.kind || in.Origin != tc.origin || in.Body != tc.body {
				t.Errorf("got %s/%s %q, want %s/%s %q", in.Kind, in.Origin, in.Body, tc.kind, tc.origin, tc.body)
			}
		})
	}
}

func TestResolve_RefusesAnEmptyInput(t *testing.T) {
	for _, tc := range []struct{ name, arg, stdin string }{
		{"empty", "", ""}, {"blank", "   ", ""}, {"blank stdin", "-", "  \n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Resolve(context.Background(), tc.arg, strings.NewReader(tc.stdin), nil, nil); !errors.Is(err, ErrNoInput) {
				t.Errorf("err = %v, want ErrNoInput", err)
			}
		})
	}
}

// A single-line, whitespace-free, path-shaped argument that does not exist is
// read as text with a high-severity warning naming it; prose, multi-line input
// and an existing directory are not.
func TestResolve_FlagsAMistypedPath(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "src")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		arg     string
		flagged bool
	}{
		{"widget/report.txt", true},
		{"report.txt", true},
		{"nope/", true},
		{"the widget counter double-counts on retry", false},
		{"see widget/report.txt for details", false},
		{"widget/report.txt\nsecond line", false},
		{"version.1234567", false},
		{"plainword", false},
		{"end.", false},
		{existing, false},
		{existing + "/", false},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			run := NewRun("tool", "test")
			in, err := Resolve(context.Background(), tc.arg, nil, nil, run)
			if err != nil || in.Kind != KindText {
				t.Fatalf("Resolve = %s, %v; want text", in.Kind, err)
			}
			ws := warningsOf(run.Warnings(), WarnInputLooksLikePath)
			if !tc.flagged {
				if len(ws) != 0 {
					t.Errorf("flagged as a path: %+v", ws)
				}
				return
			}
			if len(ws) != 1 || ws[0].Severity != "high" || ws[0].Stage != "input" || !strings.Contains(ws[0].Message, tc.arg) {
				t.Errorf("warnings = %+v, want one high input warning naming the argument", ws)
			}
		})
	}
}

// An issue is read through the forge client with its comments, where the
// useful part of a report usually is.
func TestResolve_ReadsAnIssueWithItsComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/comments") {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"body": "here is the traceback", "user": map[string]any{"login": "maintainer"}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 42, "title": "it crashes", "body": "on save", "state": "open",
			"user": map[string]any{"login": "reporter"},
		})
	}))
	defer srv.Close()

	client, err := issuex.NewWithOptions(issuex.Options{
		BaseURL:   srv.URL,
		Repo:      issuex.Repo{Owner: "acme", Name: "widgets", Host: "github.com"},
		Token:     "t",
		UserAgent: "test",
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	in, err := Resolve(context.Background(), "https://github.com/acme/widgets/issues/42", nil, client, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if in.Kind != KindIssue || in.Issue == nil || in.Issue.Number != 42 {
		t.Fatalf("input = %s, issue %+v", in.Kind, in.Issue)
	}
	for _, want := range []string{"it crashes", "on save", "here is the traceback", "maintainer"} {
		if !strings.Contains(in.Body, want) {
			t.Errorf("Body is missing %q:\n%s", want, in.Body)
		}
	}
}

// Every shape of forge URL is an issue: a GitHub issue and pull request, a
// GitLab issue and merge request, each with its repository, number and
// pull-request flag.
func TestResolve_RecognisesEveryForgeURLShape(t *testing.T) {
	client := &mockForgeClient{
		authenticated: true,
		readIssueFn: func(_ context.Context, ref issuex.IssueRef) (issuex.IssueThread, error) {
			return issuex.IssueThread{Issue: issuex.Issue{
				Number: ref.Number, Title: fmt.Sprintf("Issue %d", ref.Number), State: "open",
				Author: issuex.User{Login: "author1"}, IsPR: ref.IsPullRequest,
			}}, nil
		},
	}
	for _, tc := range []struct {
		url         string
		owner, name string
		number      int
		isPR        bool
	}{
		{"https://github.com/org/repo/issues/42", "org", "repo", 42, false},
		{"https://github.com/org/repo/pull/17", "org", "repo", 17, true},
		{"https://gitlab.com/group/sub/project/-/issues/5", "group/sub", "project", 5, false},
		{"https://gitlab.com/group/sub/project/-/merge_requests/99", "group/sub", "project", 99, true},
	} {
		t.Run(tc.url, func(t *testing.T) {
			in, err := Resolve(context.Background(), tc.url, nil, client, nil)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if in.Kind != KindIssue || in.Issue == nil || in.Thread == nil {
				t.Fatalf("Kind = %s, Issue = %+v, Thread = %+v", in.Kind, in.Issue, in.Thread)
			}
			got := in.Issue
			if got.Repo.Owner != tc.owner || got.Repo.Name != tc.name || got.Number != tc.number || got.IsPullRequest != tc.isPR {
				t.Errorf("parsed %+v, want %s/%s#%d IsPullRequest=%v", got, tc.owner, tc.name, tc.number, tc.isPR)
			}
			if in.Thread.Issue.Number != tc.number || in.Origin != got.URL() {
				t.Errorf("thread #%d, origin %q", in.Thread.Issue.Number, in.Origin)
			}
		})
	}
}

// An issue that cannot be read is an error; a 404 to a client without a
// credential says the repository may be private.
func TestResolve_AnIssueThatCannotBeRead(t *testing.T) {
	const url = "https://github.com/private/repo/issues/1"
	for _, tc := range []struct {
		name     string
		forge    issuex.Client
		mention  string
		notFound bool
	}{
		{"no forge client", nil, "cannot read issue: no forge client configured", false},
		{"not found without a credential", &mockForgeClient{readErr: issuex.ErrNotFound}, "credentials", true},
		{"not found with a credential", &mockForgeClient{authenticated: true, readErr: issuex.ErrNotFound}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(context.Background(), url, nil, tc.forge, nil)
			if err == nil || !strings.Contains(err.Error(), tc.mention) {
				t.Fatalf("err = %v, want one naming %q", err, tc.mention)
			}
			if tc.notFound && !errors.Is(err, issuex.ErrNotFound) {
				t.Errorf("err = %v does not wrap ErrNotFound", err)
			}
			if tc.mention == "" && strings.Contains(err.Error(), "credentials") {
				t.Errorf("an authenticated client is told to bring credentials: %v", err)
			}
		})
	}
}

// A thread read as an issue that is a pull request is marked one, and a
// thread over the input bound is cut and says so.
func TestResolve_APullRequestThreadIsMarkedAndTruncated(t *testing.T) {
	client := &mockForgeClient{
		authenticated: true,
		thread: issuex.IssueThread{
			Issue: issuex.Issue{Number: 5, Title: "Big PR", State: "open", Author: issuex.User{Login: "pr-author"}, IsPR: true},
			Comments: []issuex.Comment{{
				Body: strings.Repeat("a line of lengthy traceback information\n", 8000),
				User: issuex.User{Login: "bot"},
			}},
		},
	}
	in, err := Resolve(context.Background(), "https://github.com/org/repo/issues/5", nil, client, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if in.Kind != KindIssue || in.Issue == nil || !in.Issue.IsPullRequest || in.Origin != in.Issue.URL() {
		t.Errorf("input = %s, issue %+v, origin %q", in.Kind, in.Issue, in.Origin)
	}
	if !in.Truncated || len(in.Body) > MaxInputBytes+200 || !strings.Contains(in.Body, "truncated at") {
		t.Errorf("truncated = %v, %d bytes", in.Truncated, len(in.Body))
	}
}

// --input-kind refuses a mismatch as a usage error naming why, before anything
// is fetched: with no forge client, entering the issue branch would fail with
// another error.
func TestResolveForced_RefusesAMismatch(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, kind, arg, mention string
	}{
		{"missing file", "file", "/no/such/path.txt", "does not exist"},
		{"a URL as a file", "file", "https://github.com/a/b/issues/1", "does not exist"},
		{"a directory as a file", "file", dir, "is a directory"},
		{"text as an issue", "issue", "not a url", "is not a GitHub or GitLab issue"},
		{"another host as an issue", "issue", "https://example.com/not/a/forge/issues/1", "is not a GitHub or GitLab issue"},
		{"a file as stdin", "stdin", "file.txt", `must be "-"`},
		{"an unknown kind", "url", "x", "--input-kind must be one of"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveForced(context.Background(), tc.kind, tc.arg, strings.NewReader("piped"), nil)
			var ue *UsageError
			if !errors.As(err, &ue) || ue.Category() != "usage" || !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("err = %v, want a usage error naming %q", err, tc.mention)
			}
		})
	}
	for _, kind := range []string{"text", "file"} {
		t.Run("empty "+kind, func(t *testing.T) {
			if _, err := ResolveForced(context.Background(), kind, "  ", nil, nil); !errors.Is(err, ErrNoInput) {
				t.Errorf("err = %v, want ErrNoInput", err)
			}
		})
	}
}

// A forced kind is read as that kind. Text is the argument verbatim, untrimmed,
// and neither a file of that name nor a parseable URL is read.
func TestResolveForced_ReadsTheForcedKind(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("report.txt", []byte("the file's own content"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs("report.txt")
	if err != nil {
		t.Fatal(err)
	}
	thread := issuex.IssueThread{Issue: issuex.Issue{Number: 3, Title: "t", State: "open"}}
	for _, tc := range []struct {
		name, kind, arg, stdin string
		want                   SourceKind
		body                   string
	}{
		{"text naming a file", "text", "report.txt", "", KindText, "report.txt"},
		{"text that is a URL", "text", "https://github.com/a/b/issues/1", "", KindText, "https://github.com/a/b/issues/1"},
		{"text verbatim", "text", " missing/file.go ", "", KindText, " missing/file.go "},
		{"file", "file", abs, "", KindFile, "the file's own content"},
		{"stdin", "stdin", "-", "piped", KindStdin, "piped"},
		{"issue", "issue", "https://github.com/a/b/issues/3", "", KindIssue, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := ResolveForced(context.Background(), tc.kind, tc.arg, strings.NewReader(tc.stdin), &mockForgeClient{thread: thread})
			if err != nil {
				t.Fatal(err)
			}
			if in.Kind != tc.want || (tc.body != "" && in.Body != tc.body) {
				t.Errorf("input = %s %q, want %s %q", in.Kind, in.Body, tc.want, tc.body)
			}
			if tc.kind == "text" && in.Issue != nil {
				t.Error("forced text was read as an issue")
			}
		})
	}
}

// A truncated report must say so: one that ends mid-stack-trace with no
// marker reads to a model as a complete stack trace that had no more frames.
func TestTruncate_MarksTheCut(t *testing.T) {
	long := strings.Repeat("a line of log output\n", MaxInputBytes/10)
	got, cut := Truncate(long)
	if !cut || len(got) > MaxInputBytes+200 || !strings.Contains(got, "truncated at") {
		t.Errorf("truncated = %v, %d bytes, marked = %v", cut, len(got), strings.Contains(got, "truncated at"))
	}
	short := "one line\n"
	if got, cut := Truncate(short); cut || got != short {
		t.Errorf("short input was altered: %q, %v", got, cut)
	}
}

// The header names the forge by its host and the kind of thread.
func TestRenderThread_Header(t *testing.T) {
	thread := issuex.IssueThread{Issue: issuex.Issue{State: "open"}}
	for _, tc := range []struct {
		host   string
		number int
		pr     bool
		want   string
	}{
		{"gitlab.com", 1, true, "GitLab pull request o/r#1 (state: open)\n"},
		{"github.com", 2, false, "GitHub issue o/r#2 (state: open)\n"},
		{"", 3, false, "GitHub issue o/r#3 (state: open)\n"},
		{"forgejo.example.com", 4, true, "Forge pull request o/r#4 (state: open)\n"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			ref := issuex.IssueRef{Repo: issuex.Repo{Host: tc.host, Owner: "o", Name: "r"}, Number: tc.number, IsPullRequest: tc.pr}
			if got := RenderThread(ref, thread); !strings.HasPrefix(got, tc.want) {
				t.Errorf("header = %q", got)
			}
		})
	}
}

// The thread is rendered the same way every time, so a provider's cache prefix
// survives: title, author, labels when there are any, body, then each comment
// in order.
func TestRenderThread_IsDeterministicAndOrdered(t *testing.T) {
	for i, tc := range []issuex.IssueThread{
		{
			Issue: issuex.Issue{Title: "Crash in parser", Author: issuex.User{Login: "alice"}, State: "open",
				Body: "Steps to reproduce:\n1. run app\n2. crash", Labels: []string{"bug", "critical"}},
			Comments: []issuex.Comment{
				{User: issuex.User{Login: "bob"}, Body: "Confirmed on macOS"},
				{User: issuex.User{Login: "carol"}, Body: "Fixed in PR #12"},
			},
		},
		{
			Issue:    issuex.Issue{Title: "Feature request: dark mode", Author: issuex.User{Login: "david"}, State: "closed", Body: "Please add dark mode."},
			Comments: []issuex.Comment{{User: issuex.User{Login: "eve"}, Body: "Shipped in v2.0"}},
		},
		{Issue: issuex.Issue{Title: "Empty issue", Author: issuex.User{Login: "frank"}, State: "open"}},
	} {
		t.Run(tc.Issue.Title, func(t *testing.T) {
			ref := issuex.IssueRef{Repo: issuex.Repo{Host: "github.com", Owner: "org", Name: "repo"}, Number: i + 1}
			out := RenderThread(ref, tc)
			if out != RenderThread(ref, tc) {
				t.Fatal("RenderThread is not deterministic")
			}
			parts := []string{"Title: " + tc.Issue.Title, "Author: " + tc.Issue.Author.Login}
			if len(tc.Issue.Labels) > 0 {
				parts = append(parts, "Labels: "+strings.Join(tc.Issue.Labels, ", "))
			} else if strings.Contains(out, "Labels:") {
				t.Error("a Labels line without labels")
			}
			if tc.Issue.Body != "" {
				parts = append(parts, tc.Issue.Body)
			}
			for _, c := range tc.Comments {
				parts = append(parts, fmt.Sprintf("--- comment by %s ---\n%s", c.User.Login, c.Body))
			}
			last := -1
			for _, p := range parts {
				i := strings.Index(out, p)
				if i < 0 {
					t.Errorf("missing %q", p)
					continue
				}
				if i <= last {
					t.Errorf("%q is out of order", p)
				}
				last = i
			}
		})
	}
}

// A thread with more comments than were read says so at its end.
func TestRenderThread_MarksUnreadComments(t *testing.T) {
	ref := issuex.IssueRef{Repo: issuex.Repo{Host: "gitlab.com", Owner: "g", Name: "p"}, Number: 10}
	const notice = "\n[... further comments not read ...]\n"
	for _, truncated := range []bool{true, false} {
		out := RenderThread(ref, issuex.IssueThread{
			Issue: issuex.Issue{Title: "Bug", State: "open", Author: issuex.User{Login: "alice"}}, Truncated: truncated,
		})
		if strings.HasSuffix(out, notice) != truncated {
			t.Errorf("truncated=%v: out = %q", truncated, out)
		}
	}
}
