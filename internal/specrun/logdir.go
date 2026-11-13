package specrun

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// prepareLogDir resolves the log directory, makes sure it can be written, and
// makes the run's own directory under runs/, named by a new run ID, where its
// sessions' files and run.json go. One inside the working tree is a usage
// error, since af-impl refuses a dirty tree and the first log would stop the
// first package; one that cannot be written stops the run at stage preflight.
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
	runs := filepath.Join(dir, runsDirName)
	if err := os.MkdirAll(runs, 0o755); err != nil {
		return envelope.ExitFailed, failure("preflight", "environment", "the runs directory %s could not be created: %v", runs, err)
	}
	// Two runs that start in the same second may draw the same digits:
	// a directory that exists already is never shared.
	var id string
	for range 8 {
		id = newRunID(r.started)
		if err = os.Mkdir(filepath.Join(runs, id), 0o755); !errors.Is(err, fs.ErrExist) {
			break
		}
	}
	if err != nil {
		return envelope.ExitFailed, failure("preflight", "environment", "the run's directory under %s could not be created: %v", runs, err)
	}
	r.logDir, r.runID, r.runDir = dir, id, filepath.Join(runs, id)
	r.result.RunID, r.result.RunDir = id, r.runDir
	r.runRecord = &runRecord{RunID: id, StartedAt: r.started.UTC().Format(time.RFC3339), PID: os.Getpid(),
		Settings: r.opts.settings(dir), Versions: r.result.Versions}
	r.saveRunRecord()
	fmt.Fprintf(r.stderr, "%s: run %s; its logs are in %s\n", tool, id, r.runDir)
	return 0, nil
}

// runsDirName is the directory of the log directory that holds one directory
// per run, named by its run ID.
const runsDirName = "runs"

// newRunID names a run: the UTC time it started, to the second, then four
// hex digits drawn at random. The runs' directories sort by when they
// started, and a person can read the time off the name.
func newRunID(started time.Time) string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return started.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:])
}

// runRecord is run.json in a run's directory: written when the run starts,
// and again when it ends, then with the envelope it wrote. A run.json with no
// ended_at belongs to a run that did not end, as after a crash or a kill.
type runRecord struct {
	RunID     string          `json:"run_id"`
	StartedAt string          `json:"started_at"`
	EndedAt   string          `json:"ended_at,omitempty"`
	PID       int             `json:"pid"`
	Settings  runSettings     `json:"settings"`
	Versions  *Versions       `json:"versions,omitempty"`
	Envelope  json.RawMessage `json:"envelope,omitempty"`
}

// runSettings are the settings a run ran with, flags and variables resolved.
type runSettings struct {
	Max            int      `json:"max"`
	Stall          string   `json:"stall"`
	LogDir         string   `json:"log_dir"`
	ReconcileEvery int      `json:"reconcile_every"`
	StopOn         []string `json:"stop_on"`
	Repair         bool     `json:"repair"`
	RepairOn       []string `json:"repair_on"`
	RepairMax      int      `json:"repair_max"`
	RepairSecurity bool     `json:"repair_security"`
	StrictSkills   bool     `json:"strict_skills"`
	StrictHandoffs bool     `json:"strict_handoffs"`
}

// settings are the options as a run records them, with the log directory
// they resolved to.
func (o *options) settings(logDir string) runSettings {
	return runSettings{Max: o.max, Stall: o.stall.String(), LogDir: logDir, ReconcileEvery: o.reconcileEvery,
		StopOn: o.stopOn, Repair: o.repair, RepairOn: o.repairOn, RepairMax: o.repairMax,
		RepairSecurity: o.repairSecurity, StrictSkills: o.strictSkills, StrictHandoffs: o.strictHandoffs}
}

// saveRunRecord writes run.json in the run's directory.
func (r *run) saveRunRecord() {
	if r.runRecord == nil {
		return
	}
	path := filepath.Join(r.runDir, runRecordName)
	if err := writeAtomic(path, r.runRecord); err != nil {
		fmt.Fprintf(r.stderr, "%s: %s could not be written: %v\n", tool, path, err)
	}
}

const runRecordName = "run.json"

// endRunRecord records in run.json that the run ended, with the envelope it
// wrote.
func (r *run) endRunRecord(env []byte) {
	if r.runRecord == nil {
		return
	}
	// The run is timed by the process's clock, as the envelope is; the
	// host's clock times the steps.
	r.runRecord.EndedAt = time.Now().UTC().Format(time.RFC3339)
	r.runRecord.Envelope = json.RawMessage(env)
	r.saveRunRecord()
}

// stepName is the name of a step's files in the log directory: its number,
// its kind and its subject.
func stepName(n int, kind, subject string) string {
	return fmt.Sprintf("%04d-%s-%s", n, kind, subject)
}
