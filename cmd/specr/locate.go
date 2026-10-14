package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// locate turns the one positional argument into the absolute directory of a
// spec package.
//
// The argument is a reference, not a document. It is tried as a path first —
// the package directory, or a file inside it — relative to the working
// directory and then to root. Then it is matched against the directory
// names under the spec root, as the whole name (16_indexed_code_search), the
// number (16, or 016) or the name after it (indexed_code_search). A
// reference that matches two packages is refused naming both, and one that
// matches none is refused naming what is there. Every refusal is a usage
// error: nothing was read.
func locate(ref, root, specsDir string) (string, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "-":
		return "", cli.Usagef("specr reads a spec package from disk, not stdin: give its directory, id or name")
	case strings.Contains(ref, "://"):
		return "", cli.Usagef("specr takes a spec package, not a URL: %s; give its directory, id or name", ref)
	case strings.ContainsAny(ref, "\r\n"):
		return "", cli.Usagef("the input is several lines; specr takes one reference to a spec package")
	}

	var notPackage string
	for _, candidate := range pathCandidates(ref, root) {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		dir := candidate
		if info.Mode().IsRegular() {
			dir = filepath.Dir(candidate)
		}
		if isSpecDir(dir) {
			return dir, nil
		}
		if notPackage == "" {
			notPackage = candidate
		}
	}

	notFound := fmt.Sprintf("%s names no spec package", ref)
	if notPackage != "" {
		notFound = fmt.Sprintf("%s is not a spec package or a file in one (no prd.md there)", displayPath(notPackage, root))
	}
	shownRoot := displayPath(specsDir, root)
	entries, err := specDirs(specsDir)
	if err != nil {
		return "", cli.Usagef("%s, and the spec root %s could not be read: %v", notFound, shownRoot, err)
	}
	matches := matching(entries, ref)
	switch len(matches) {
	case 1:
		return filepath.Join(specsDir, matches[0]), nil
	case 0:
		msg := fmt.Sprintf("%s; the spec root %s holds %s", notFound, shownRoot, listed(entries))
		archive := filepath.Join(specsDir, "archive")
		if archived, aerr := specDirs(archive); aerr == nil {
			if m := matching(archived, ref); len(m) > 0 {
				msg += fmt.Sprintf("; an archived package matches: give its path, %s", displayPath(filepath.Join(archive, m[0]), root))
			}
		}
		return "", cli.Usagef("%s", msg)
	default:
		return "", cli.Usagef("%s names %d spec packages (%s); give the directory name",
			ref, len(matches), strings.Join(matches, ", "))
	}
}

func pathCandidates(ref, root string) []string {
	if filepath.IsAbs(ref) {
		return []string{filepath.Clean(ref)}
	}
	var out []string
	if abs, err := filepath.Abs(ref); err == nil {
		out = append(out, abs)
	}
	return append(out, filepath.Join(root, ref))
}

// isSpecDir reports whether dir holds a prd.md: the one file every package,
// valid or not, has.
func isSpecDir(dir string) bool {
	prd, err := os.Stat(filepath.Join(dir, "prd.md"))
	return err == nil && prd.Mode().IsRegular()
}

// specDirs lists the NN_name directories under root, sorted.
func specDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && afspec.IsSpecDirName(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// matching returns the directory names ref names: the whole name, its
// number (compared as a number, so 9 and 09 are the same), or its name.
func matching(dirs []string, ref string) []string {
	var out []string
	for _, d := range dirs {
		num, name, err := afspec.ParseSpecDirName(d)
		if err != nil {
			continue
		}
		if d == ref || name == ref || sameNumber(num, ref) {
			out = append(out, d)
		}
	}
	return out
}

func sameNumber(a, b string) bool {
	x, err := strconv.Atoi(a)
	if err != nil {
		return false
	}
	y, err := strconv.Atoi(b)
	return err == nil && x == y
}

func listed(dirs []string) string {
	if len(dirs) == 0 {
		return "no spec packages"
	}
	return strings.Join(dirs, ", ")
}
