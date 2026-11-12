package gate

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Top is the absolute path of the top level of the git work tree that holds
// dir.
func Top(dir string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", errors.New("git is not installed")
	}
	top, err := gitOut(dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not in a git work tree", dir)
	}
	return top, nil
}

// Commit is the full id of the commit rev names in the repository at top.
func Commit(top, rev string) (string, error) {
	if strings.TrimSpace(rev) == "" || strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("%q names no commit", rev)
	}
	id, err := gitOut(top, nil, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil || id == "" {
		return "", fmt.Errorf("%q names no commit of the repository", rev)
	}
	return id, nil
}

// BaseBranch is the branch work on the repository at top is merged into: the
// one refs/remotes/origin/HEAD names, else main. It fails when there is
// neither.
func BaseBranch(top string) (string, error) {
	if ref, err := gitOut(top, nil, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return ref, nil
	}
	if _, err := gitOut(top, nil, "rev-parse", "--verify", "--quiet", "refs/heads/main^{commit}"); err == nil {
		return "refs/heads/main", nil
	}
	return "", errors.New("neither refs/remotes/origin/HEAD nor a branch main exists")
}

// MergeBase is the merge base of HEAD and base in the repository at top.
func MergeBase(top, base string) (string, error) {
	id, err := gitOut(top, nil, "merge-base", "HEAD", base)
	if err != nil || id == "" {
		return "", fmt.Errorf("HEAD and %s have no merge base", base)
	}
	return id, nil
}

// Since is the set of commits a run counts at: since itself, and every
// descendant of since that HEAD reaches, and while a merge is uncommitted,
// that MERGE_HEAD reaches.
func Since(top, since string) (map[string]bool, error) {
	in := map[string]bool{since: true}
	tips := []string{"HEAD"}
	if _, err := gitOut(top, nil, "rev-parse", "--verify", "--quiet", "MERGE_HEAD^{commit}"); err == nil {
		tips = append(tips, "MERGE_HEAD")
	}
	for _, tip := range tips {
		if _, err := gitOut(top, nil, "rev-parse", "--verify", "--quiet", tip+"^{commit}"); err != nil {
			continue // no commit yet
		}
		out, err := gitOut(top, nil, "rev-list", "--ancestry-path", since+".."+tip)
		if err != nil {
			return nil, err
		}
		for _, id := range strings.Fields(out) {
			in[id] = true
		}
	}
	return in, nil
}
