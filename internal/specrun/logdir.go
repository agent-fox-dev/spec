package specrun

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agent-fox-dev/aflib/envelope"
)

// logDirFor is the log directory: --log-dir, else agent-fox/specrun/<the
// repository's name> under the user's cache directory ($XDG_CACHE_HOME when it
// is an absolute path). It is not created here.
func (r *run) logDirFor() (string, error) {
	if r.opts.logDir != "" {
		if filepath.IsAbs(r.opts.logDir) {
			return filepath.Clean(r.opts.logDir), nil
		}
		return filepath.Join(r.host.dir, r.opts.logDir), nil
	}
	cache := r.host.getenv("XDG_CACHE_HOME")
	if cache == "" || !filepath.IsAbs(cache) {
		var err error
		if cache, err = os.UserCacheDir(); err != nil {
			return "", err
		}
	}
	return filepath.Join(cache, "agent-fox", "specrun", filepath.Base(r.root)), nil
}

// inside reports whether path is root or under it, symbolic links resolved as
// far as the path exists.
func inside(path, root string) bool {
	rel, err := filepath.Rel(resolve(root), resolve(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolve resolves the symbolic links of the longest part of path that exists.
func resolve(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for p := path; ; p = filepath.Dir(p) {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(real, rest)
		}
		if filepath.Dir(p) == p {
			return path
		}
		rest = filepath.Join(filepath.Base(p), rest)
	}
}

// prepareLogDir resolves the log directory and makes sure it can be written.
// One inside the working tree is a usage error, since af-impl refuses a dirty
// tree and the first log would stop the first package; one that cannot be
// written stops the run at stage preflight.
func (r *run) prepareLogDir() (int, *envelope.ErrorInfo) {
	dir, err := r.logDirFor()
	if err != nil {
		return envelope.ExitFailed, failure("preflight", "environment", "the log directory could not be found: %v", err)
	}
	if inside(dir, r.root) {
		return envelope.ExitUsage, failure("usage", envelope.CategoryUsage,
			"%s %s is inside the repository's working tree, and af-impl refuses a dirty tree: give a directory outside it", r.opts.logDirFrom, dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return envelope.ExitFailed, failure("preflight", "environment", "the log directory %s could not be created: %v", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return envelope.ExitFailed, failure("preflight", "environment", "the log directory %s cannot be written: %v", dir, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	r.logDir = dir
	return 0, nil
}

// stepName is the name of a step's files in the log directory: its number,
// its kind and its subject.
func stepName(n int, kind, subject string) string {
	return fmt.Sprintf("%04d-%s-%s", n, kind, subject)
}
