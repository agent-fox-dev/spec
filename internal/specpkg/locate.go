package specpkg

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// Locate turns the one positional argument of a command into the absolute
// directory of a spec package. tool is the command that was called, specr or
// speca, and every refusal begins with it.
//
// The argument is a reference, not a document. It is tried as a path first —
// the package directory, or a file inside it — relative to the working
// directory and then to root. Then it is matched against the directory
// names under the spec root, as the whole name (16_indexed_code_search), the
// number (16, or 016) or the name after it (indexed_code_search). A
// reference that matches two packages is refused naming both, and one that
// matches none is refused naming what is there, and the archived package it
// matches, which is reached by its path only. Every refusal is a usage error:
// nothing was read.
func Locate(tool, ref, root, specsDir string) (string, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "-":
		return "", cli.Usagef("%s reads a spec package from disk, not stdin: give its directory, id or name", tool)
	case strings.Contains(ref, "://"):
		return "", cli.Usagef("%s takes a spec package, not a URL: %s; give its directory, id or name", tool, ref)
	case strings.ContainsAny(ref, "\r\n"):
		return "", cli.Usagef("%s takes one reference to a spec package, not several lines", tool)
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

	refused := tool + " cannot resolve the reference: "
	notFound := fmt.Sprintf("%s names no spec package", ref)
	if notPackage != "" {
		notFound = fmt.Sprintf("%s is not a spec package or a file in one (no prd.md there)", Display(notPackage, root))
	}
	shownRoot := Display(specsDir, root)
	entries, err := List(specsDir)
	if err != nil {
		return "", cli.Usagef("%s%s, and the spec root %s could not be read: %v", refused, notFound, shownRoot, err)
	}
	matches := matching(entries, ref)
	switch len(matches) {
	case 1:
		return filepath.Join(specsDir, matches[0]), nil
	case 0:
		msg := fmt.Sprintf("%s%s; the spec root %s holds %s", refused, notFound, shownRoot, listed(entries))
		archive := filepath.Join(specsDir, "archive")
		if archived, aerr := List(archive); aerr == nil {
			if m := matching(archived, ref); len(m) > 0 {
				msg += fmt.Sprintf("; an archived package matches: give its path, %s", Display(filepath.Join(archive, m[0]), root))
			}
		}
		return "", cli.Usagef("%s", msg)
	default:
		return "", cli.Usagef("%s%s names %d spec packages (%s); give the directory name",
			refused, ref, len(matches), strings.Join(matches, ", "))
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

// List returns the names of the NN_name directories under root, in the order
// of their numbers: the prefix compared as a number, so that 99_y comes before
// 100_x, and a tie broken by the whole name. It lists directories whether or
// not they hold a package that loads.
func List(root string) ([]string, error) {
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
	slices.SortFunc(out, compareDirs)
	return out, nil
}

// compareDirs orders two NN_name directory names by their numbers. The
// numbers are compared as digit strings without their leading zeros, shorter
// first, so that a number too long for an int still orders.
func compareDirs(a, b string) int {
	na, _, _ := strings.Cut(a, "_")
	nb, _, _ := strings.Cut(b, "_")
	na, nb = strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
	if c := cmp.Compare(len(na), len(nb)); c != 0 {
		return c
	}
	if c := strings.Compare(na, nb); c != 0 {
		return c
	}
	return strings.Compare(a, b)
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
