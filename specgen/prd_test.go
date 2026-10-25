package specgen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/agent-fox-dev/agentkit-go/workspace"
)

// submitPRD drives the real tool handler with the arguments a model would
// have produced, in a workspace holding a.go and b.go and a directory d.
func submitPRD(t *testing.T, ws *workspace.Workspace, args map[string]any) (*prdSink, string, bool) {
	t.Helper()
	var sink prdSink
	res := submitPRDTool(&sink, ws).Execute(context.Background(), mustJSON(t, args))
	return &sink, res.Error + " " + res.Detail, res.OK && res.Terminate
}

func prdWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	ws := newWorkspace(t)
	writeFiles(t, ws.Root(), map[string]string{"a.go": "package a\n", "b.go": "package a\n"})
	if err := os.Mkdir(filepath.Join(ws.Root(), "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	return ws
}

func scopes(names ...string) []map[string]any {
	var out []map[string]any
	for _, n := range names {
		out = append(out, map[string]any{"name": n, "scope": "Covers " + n + "."})
	}
	return out
}

func relevant(paths ...string) []map[string]any {
	var out []map[string]any
	for _, p := range paths {
		out = append(out, map[string]any{"path": p, "why": "relevant for testing"})
	}
	return out
}

func TestSubmitPRD_AcceptsAFinishedPRD(t *testing.T) {
	ws := prdWorkspace(t)
	for _, tc := range []struct {
		name  string
		set   func(map[string]any)
		check func(*testing.T, PRD)
	}{
		{"a plain PRD, its name and title trimmed", func(a map[string]any) {
			a["spec_name"], a["title"] = " widget_counter ", " Widget Counter "
		}, func(t *testing.T, p PRD) {
			if p.SpecName != "widget_counter" || p.Title != "Widget Counter" || len(p.RelevantFiles) != 0 {
				t.Errorf("got %+v", p)
			}
		}},
		{"relevant files that are regular files", func(a map[string]any) { a["relevant_files"] = relevant("a.go", "b.go") },
			func(t *testing.T, p PRD) {
				if len(p.RelevantFiles) != 2 {
					t.Errorf("RelevantFiles = %+v", p.RelevantFiles)
				}
			}},
		{"an empty list of relevant files", func(a map[string]any) { a["relevant_files"] = []any{} }, func(*testing.T, PRD) {}},
		{"a well-formed split", func(a map[string]any) {
			a["recommended_split"] = scopes("widget_counter", "widget_store", "widget_report")
		}, func(t *testing.T, p PRD) {
			if len(p.RecommendedSplit) != 3 {
				t.Errorf("RecommendedSplit = %+v", p.RecommendedSplit)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := validPRDArgs()
			tc.set(args)
			sink, msg, ok := submitPRD(t, ws, args)
			if !ok {
				t.Fatalf("rejected: %s", msg)
			}
			got, recorded := sink.get()
			if !recorded {
				t.Fatal("nothing was recorded")
			}
			tc.check(t, got)
		})
	}
}

// Each check is one that would otherwise fail later and more expensively — a
// name the directory rule refuses fails after three more paid phases, a body
// without the hashed Intent section fails at activation — and each is
// repairable on the next turn, so each is a tool error naming what to fix.
func TestSubmitPRD_RejectsWhatWouldFailLater(t *testing.T) {
	ws := prdWorkspace(t)
	for _, tc := range []struct {
		name     string
		set      func(map[string]any)
		code     string
		mentions []string
	}{
		{"a spec name the format cannot use", func(a map[string]any) { a["spec_name"] = "Widget Counter" },
			"invalid_spec_name", []string{"[a-z][a-z0-9]*(_[a-z0-9]+)*"}},
		{"a spec name with a trailing underscore", func(a map[string]any) { a["spec_name"] = "issuex_" },
			"invalid_spec_name", []string{"issuex_"}},
		{"an empty title", func(a map[string]any) { a["title"] = "  " }, "empty_title", []string{"title is empty"}},
		{"no Intent section", func(a map[string]any) { a["body"] = "# Widget Counter\n\n## Goals\n\n- Count.\n" },
			"missing_intent", []string{"## Intent"}},
		{"an Intent heading activation cannot hash", func(a map[string]any) { a["body"] = "## intent\n\nWhy.\n" },
			"missing_intent", []string{"## Intent"}},
		{"frontmatter the model added anyway", func(a map[string]any) {
			a["body"] = "---\ntitle: x\n---\n\n## Intent\n\nCount widgets.\n"
		}, "frontmatter_in_body", []string{"frontmatter"}},
		{"a split of one scope", func(a map[string]any) { a["recommended_split"] = scopes("widget_counter") },
			"invalid_split", []string{"at least two"}},
		{"a scope name the format cannot use", func(a map[string]any) {
			a["recommended_split"] = scopes("widget_counter", "Widget Store")
		}, "invalid_split", []string{"recommended_split[1].name", "[a-z][a-z0-9]*(_[a-z0-9]+)*"}},
		{"a scope name used twice", func(a map[string]any) {
			a["recommended_split"] = scopes("widget_counter", "widget_counter")
		}, "invalid_split", []string{"twice"}},
		{"a first scope that is not the PRD submitted", func(a map[string]any) {
			a["recommended_split"] = scopes("widget_store", "widget_counter")
		}, "invalid_split", []string{"must be the same name"}},
		{"an empty scope", func(a map[string]any) {
			a["recommended_split"] = []map[string]any{{"name": "widget_counter", "scope": "Counting."}, {"name": "widget_store", "scope": " "}}
		}, "invalid_split", []string{"empty scope"}},
		{"a relevant file that does not exist", func(a map[string]any) { a["relevant_files"] = relevant("a.go", "nonexistent.go") },
			"invalid_relevant_files", []string{"nonexistent.go"}},
		{"a relevant file that is a directory", func(a map[string]any) { a["relevant_files"] = relevant("d") },
			"invalid_relevant_files", []string{": d"}},
		{"a relevant file outside the workspace", func(a map[string]any) { a["relevant_files"] = relevant("../outside.go") },
			"invalid_relevant_files", []string{"../outside.go"}},
		{"arguments that are not a PRD", func(a map[string]any) { a["title"] = 7 }, "invalid_arguments", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := validPRDArgs()
			tc.set(args)
			sink, msg, ok := submitPRD(t, ws, args)
			if ok {
				t.Fatal("accepted")
			}
			if _, recorded := sink.get(); recorded {
				t.Error("a rejected submission was recorded")
			}
			for _, want := range append([]string{tc.code}, tc.mentions...) {
				if !strings.Contains(msg, want) {
					t.Errorf("the rejection lacks %q: %s", want, msg)
				}
			}
		})
	}
}

// The intent check is afspec's own, on the body as it will be written: a
// looser one would pass a PRD whose activation then fails.
func TestHasIntent(t *testing.T) {
	for body, want := range map[string]bool{
		"## Intent\n\nx\n":                      true,
		"# Title\n\n## Intent  \n\nx\n":         true,
		"---\na: b\n---\n\n## Intent\n\nx\n":    true,
		"# Title\n\n##  Intent  \n\nx\n":        false,
		"# Title\n\n## intent\n\nx\n":           false,
		"# Intent\n\nx\n":                       false,
		"## Intentional design\n\nx\n":          false,
		"Intent: count widgets\n":               false,
		"# Title\n\n## Goals\n\n- the Intent\n": false,
	} {
		if got := HasIntent(body); got != want {
			t.Errorf("HasIntent(%q) = %v, want %v", body, got, want)
		}
	}
}

func TestValidSpecName(t *testing.T) {
	for _, s := range []string{"a", "widget_counter", "spec2", "a_1_b"} {
		if !ValidSpecName(s) {
			t.Errorf("ValidSpecName(%q) = false", s)
		}
	}
	for _, s := range []string{"", "Widget", "1st", "widget-counter", "widget counter", "_x", "widget__counter", "widget_"} {
		if ValidSpecName(s) {
			t.Errorf("ValidSpecName(%q) = true", s)
		}
	}
}

// The notes a PRD phase hands on are bounded, because every later phase reads
// them: cut at a line where there is one, never inside a character, and
// saying that they were cut.
func TestBoundNotes(t *testing.T) {
	if got := boundNotes("  short notes \n"); got != "short notes" {
		t.Errorf("short notes = %q", got)
	}
	const marker = "\n[…the rest of the notes was cut]"
	long := strings.Repeat("a line of notes about model.go é\n", maxCodebaseNotes/20)
	got := boundNotes(long)
	if len(got) > maxCodebaseNotes+len(marker) || !strings.HasSuffix(got, marker) || !strings.Contains(got, "é"+marker) {
		t.Errorf("long notes: %d bytes, ending %q", len(got), got[len(got)-60:])
	}
	// One long line has no line to cut at, so the cut falls where it falls,
	// backed off to a whole character.
	oneLine := strings.Repeat("é", maxCodebaseNotes)
	if got := boundNotes(oneLine); !utf8.ValidString(got) || !strings.HasSuffix(got, marker) || len(got) > maxCodebaseNotes+len(marker) {
		t.Errorf("one long line: %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
}
