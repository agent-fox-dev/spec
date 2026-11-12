package gate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Tree is the working tree a gate runs on, as git sees it.
type Tree struct {
	// Top is the absolute path of the work tree's top level.
	Top string
	// ID is git's tree id of the working tree as it is: tracked and untracked
	// files, ignored ones left out.
	ID string
	// Commit is the commit checked out, or empty when there is none yet.
	Commit string
}

// ReadTree works out the tree of the work tree that holds dir, without
// changing the repository: the index is copied to a temporary file, and the
// objects git writes go to a temporary object directory. The repository's own
// store is not made an alternate, because git refreshes the time of an object
// it finds there; write-tree takes the entries of the index without looking
// their objects up. Neither the user's index, nor the staged changes, nor
// anything under .git gains or changes a file, or its time. It fails when dir
// is not in a git work tree or git is not installed.
func ReadTree(dir string) (Tree, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return Tree{}, errors.New("git is not installed")
	}
	top, err := gitOut(dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return Tree{}, fmt.Errorf("%s is not in a git work tree", dir)
	}
	index, err := gitOut(top, nil, "rev-parse", "--git-path", "index")
	if err != nil {
		return Tree{}, err
	}
	index = absUnder(top, index)

	tmp, err := os.MkdirTemp("", "speca-gate-tree-")
	if err != nil {
		return Tree{}, err
	}
	defer removeAll(tmp)
	tmpIndex, tmpObjects := filepath.Join(tmp, "index"), filepath.Join(tmp, "objects")
	if err := os.Mkdir(tmpObjects, 0o700); err != nil {
		return Tree{}, err
	}
	if err := copyFile(index, tmpIndex); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Tree{}, err
	}
	env := []string{"GIT_INDEX_FILE=" + tmpIndex, "GIT_OBJECT_DIRECTORY=" + tmpObjects}
	if _, err := gitOut(top, env, "add", "--all", "--", "."); err != nil {
		return Tree{}, err
	}
	id, err := gitOut(top, env, "write-tree", "--missing-ok")
	if err != nil {
		return Tree{}, err
	}
	// No commit yet is a tree all the same.
	commit, _ := gitOut(top, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	return Tree{Top: top, ID: id, Commit: commit}, nil
}

// gitOut runs git in dir with env added to the environment, taking no
// optional lock and never prompting, and returns its output, trimmed.
func gitOut(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append([]string{"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0"}, env...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// absUnder makes a path git printed relative to top absolute.
func absUnder(top, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(top, p)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
