package evidence

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"
)

// goEvent is the part of an event of go test -json the runner reads
// (go doc cmd/test2json). A build failure is an event with FailedBuild set,
// or a record whose Action is build-fail.
type goEvent struct {
	Action      string
	Package     string
	Test        string
	FailedBuild string
}

// runGo runs the tests with go test: one run per package, in the order the
// packages are first named, with the test cache off and a -run pattern that
// matches exactly the tests named in it and the parents of the subtests named
// (go test runs a parent for its subtest). Each package's import path is
// resolved with go list first, and only the events of that package are read,
// so an identifier whose package is a pattern of several (./...) is missing.
// An identifier with no package or no test is missing, and runs nothing.
func (r Runner) runGo(ctx context.Context, tests []string, log io.Writer, limit time.Duration) (map[string]Status, error) {
	out := make(map[string]Status, len(tests))
	var order []string
	byPackage := map[string][]string{} // package → the test names asked of it
	for _, id := range tests {
		pkg, test, ok := splitIdentifier(id)
		if !ok {
			out[id] = Missing
			continue
		}
		if _, seen := byPackage[pkg]; !seen {
			order = append(order, pkg)
		}
		byPackage[pkg] = append(byPackage[pkg], test)
	}
	for _, pkg := range order {
		listed, err := run(ctx, r.Dir, "go list", []string{"go", "list", "-e", "-f", "{{.ImportPath}}", pkg}, log, limit)
		if err != nil {
			return nil, err
		}
		paths := strings.Fields(string(listed))
		if len(paths) != 1 {
			// A pattern names no one package: none of its tests is one test.
			for _, test := range byPackage[pkg] {
				out[pkg+":"+test] = Missing
			}
			continue
		}
		argv := []string{"go", "test", "-json", "-count=1", "-run", runPattern(byPackage[pkg]), pkg}
		stdout, err := run(ctx, r.Dir, "go test", argv, log, limit)
		if err != nil {
			return nil, err
		}
		got := readGoEvents(stdout, paths[0])
		for _, test := range byPackage[pkg] {
			out[pkg+":"+test] = got.status(test)
		}
	}
	return out, nil
}

// runPattern is a -run pattern that matches each test named, a subtest by
// each level of its name, and nothing else: the alternatives are joined by a
// | outside any parenthesis, which go test reads as a choice of whole
// patterns, each anchored at every level.
func runPattern(tests []string) string {
	alternatives := make([]string, len(tests))
	for i, test := range tests {
		levels := strings.Split(test, "/")
		for j, l := range levels {
			levels[j] = "^" + regexp.QuoteMeta(l) + "$"
		}
		alternatives[i] = strings.Join(levels, "/")
	}
	return strings.Join(alternatives, "|")
}

// goRun is what one run of go test reported: the last pass, fail or skip of
// each test, and whether the package failed to build.
type goRun struct {
	final       map[string]string
	buildFailed bool
}

// readGoEvents reads the events of a run of go test -json for the package with
// the given import path; a line that is not one, and an event of another
// package, are ignored.
func readGoEvents(stdout []byte, importPath string) goRun {
	g := goRun{final: map[string]string{}}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var e goEvent
		if json.Unmarshal(sc.Bytes(), &e) != nil || (e.Package != "" && e.Package != importPath) {
			continue
		}
		switch {
		case e.Action == "build-fail" || e.FailedBuild != "":
			g.buildFailed = true
		case e.Test != "" && (e.Action == "pass" || e.Action == "fail" || e.Action == "skip"):
			g.final[e.Test] = e.Action
		}
	}
	return g
}

// status is the status of the test named in the run: an error when the
// package did not build, missing when no event ended it.
func (g goRun) status(test string) Status {
	if g.buildFailed {
		return Error
	}
	switch g.final[test] {
	case "pass":
		return Pass
	case "fail":
		return Fail
	case "skip":
		return Skipped
	}
	return Missing
}
