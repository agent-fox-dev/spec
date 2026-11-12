package project

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// steeringPlaceholder marks a steering file that has nothing to say yet.
const steeringPlaceholder = "<!-- steering:placeholder -->"

// maxSteeringBytes bounds the steering file. A document that size is not a
// preamble, and inlining it would crowd out the code a phase has to read.
const maxSteeringBytes = 24 << 10

// Steering reads <specsDir>/steering.md, the project-level directives to every
// agent working on the repository, and returns its body: the file after its
// frontmatter, or the whole file when it has none. A file that is missing, a
// symlink, or whose body is empty, over the size bound, or only the
// placeholder or the bare heading is nothing, and the result is "".
func Steering(specsDir string) string { return ReadSteering(specsDir).Body }

// Amendment is one entry of the steering's record of its amendments: the
// version it brought the file to, the day it was made, and what changed.
type Amendment struct {
	Version string `yaml:"version"`
	Date    string `yaml:"date"`
	Change  string `yaml:"change"`
}

// SteeringFile is what ReadSteering read. Body is the directives, the file
// after its frontmatter. Version and Amendments are the frontmatter's, and
// both are empty unless the file is versioned: its frontmatter is well formed
// and its body says something. Problems names each well-formedness rule the
// frontmatter breaks, in the order the rules are checked, one sentence per
// rule; a file that breaks any is unversioned, and its body is used all the
// same.
//
// GateBudgetSeconds is the frontmatter's gate_budget_seconds, the seconds a
// full gate may take before it is a finding; 0 when it sets none or sets one
// that is no whole number above zero, which is then a problem.
type SteeringFile struct {
	Body              string
	Version           string
	Amendments        []Amendment
	Problems          []string
	GateBudgetSeconds int
}

// Versioned reports whether the file has a version a package can record.
func (f SteeringFile) Versioned() bool { return f.Version != "" }

// Malformed is the message of the steering_malformed warning for the file in
// specsDir: every rule its frontmatter breaks, and what follows from it. It is
// "" when the frontmatter breaks none.
func (f SteeringFile) Malformed(specsDir string) string {
	if len(f.Problems) == 0 {
		return ""
	}
	return fmt.Sprintf("the frontmatter of %s breaks %d rule(s), so its version is treated as absent and its directives are used as they are: %s",
		filepath.Join(specsDir, "steering.md"), len(f.Problems), strings.Join(f.Problems, "; "))
}

// steeringFrontmatter is the keys a steering frontmatter may hold; the gate
// budget is read from the raw keys, since it may be of any YAML type.
type steeringFrontmatter struct {
	Version    string      `yaml:"version"`
	Amendments []Amendment `yaml:"amendments"`
	gateBudget int
}

// gateBudgetKey is the frontmatter's key for the seconds a full gate may
// take, and defaultGateBudget the budget when it sets none.
const (
	gateBudgetKey     = "gate_budget_seconds"
	defaultGateBudget = 120
)

// wholeAboveZero is v as a whole number greater than zero, when it is one.
func wholeAboveZero(v any) (int, bool) {
	switch n := v.(type) {
	case uint64:
		if n > 0 && n <= math.MaxInt32 {
			return int(n), true
		}
	case int64:
		if n > 0 && n <= math.MaxInt32 {
			return int(n), true
		}
	case int:
		if n > 0 {
			return n, true
		}
	case float64:
		if n > 0 && n == math.Trunc(n) && n <= math.MaxInt32 {
			return int(n), true
		}
	}
	return 0, false
}

// ReadSteering reads <specsDir>/steering.md: its body, and its version and
// amendment record when it opens with frontmatter (ADR 15). A file whose
// first line is exactly three hyphens opens a frontmatter block, which ends at
// the next line of three hyphens. The checks for a file with nothing to say
// are made on the body alone, and such a file is nothing — no body, no
// version, no problem — whatever its frontmatter holds.
func ReadSteering(specsDir string) SteeringFile {
	path := filepath.Join(specsDir, "steering.md")
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return SteeringFile{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return SteeringFile{}
	}
	f := splitSteering(string(b))
	if saysNothing(f.Body) {
		return SteeringFile{}
	}
	return f
}

// saysNothing reports whether a steering body has no directives to give.
func saysNothing(body string) bool {
	if len(body) == 0 || len(body) > maxSteeringBytes || strings.Contains(body, steeringPlaceholder) {
		return true
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), "# Steering")) == ""
}

// splitSteering separates the frontmatter of a steering file from its body
// and checks the frontmatter.
func splitSteering(content string) SteeringFile {
	rest, opens := strings.CutPrefix(content, "---\n")
	if !opens {
		return SteeringFile{Body: content}
	}
	block, body, closes := strings.Cut(rest, "\n---\n")
	if !closes {
		if block, closes = strings.CutSuffix(rest, "\n---"); !closes {
			return SteeringFile{Body: content, Problems: []string{
				"the frontmatter opened by the first line, ---, has no closing --- line"}}
		}
		body = ""
	}
	f := SteeringFile{Body: body}
	fm, problems := checkSteeringFrontmatter(block)
	f.GateBudgetSeconds = fm.gateBudget
	if len(problems) > 0 {
		f.Problems = problems
		return f
	}
	f.Version, f.Amendments = fm.Version, fm.Amendments
	return f
}

// versionForm is MAJOR.MINOR.PATCH: three non-negative integers with no
// leading zeros.
var versionForm = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// IsVersion reports whether s is in the version form, MAJOR.MINOR.PATCH.
func IsVersion(s string) bool { return versionForm.MatchString(s) }

// CompareVersions compares two versions in the version form as three
// integers: -1 when a is older than b, 0 when they are equal, 1 when a is
// newer. A part that is not a number counts as 0.
func CompareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// versionParts is the three numbers of a version.
func versionParts(v string) [3]int {
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

// MajorOf is the first number of a version.
func MajorOf(v string) int { return versionParts(v)[0] }

// checkSteeringFrontmatter parses a frontmatter block and checks the
// well-formedness rules in their order, one sentence for each rule broken. A
// rule whose subject an earlier broken rule has already rejected is not
// checked, so that one fault is one problem.
func checkSteeringFrontmatter(block string) (steeringFrontmatter, []string) {
	var fm steeringFrontmatter
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return fm, []string{fmt.Sprintf("the frontmatter is not YAML holding a version and a list of amendments: %v", err)}
	}
	var keys map[string]any
	_ = yaml.Unmarshal([]byte(block), &keys)

	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	var extra []string
	for k := range keys {
		if k != "version" && k != "amendments" && k != gateBudgetKey {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		slices.Sort(extra)
		add("the frontmatter may hold only version, amendments and %s, and it also holds %s", gateBudgetKey, strings.Join(extra, ", "))
	}
	if v, ok := keys[gateBudgetKey]; ok {
		if n, whole := wholeAboveZero(v); whole {
			fm.gateBudget = n
		} else {
			add("%s must be a whole number of seconds greater than zero; it is %v, so the budget of %d seconds applies", gateBudgetKey, v, defaultGateBudget)
		}
	}
	versionOK := IsVersion(fm.Version)
	if !versionOK {
		add("version must be MAJOR.MINOR.PATCH, three non-negative integers with no leading zeros; it is %q", fm.Version)
	}
	if len(fm.Amendments) == 0 {
		add("amendments must list at least one amendment")
		return fm, problems
	}

	var badVersion, badDate, noChange []string
	for i, a := range fm.Amendments {
		if !IsVersion(a.Version) {
			badVersion = append(badVersion, fmt.Sprintf("amendment %d (%q)", i+1, a.Version))
		}
		if _, err := time.Parse(time.DateOnly, a.Date); err != nil {
			badDate = append(badDate, fmt.Sprintf("amendment %d (%q)", i+1, a.Date))
		}
		if strings.TrimSpace(a.Change) == "" {
			noChange = append(noChange, fmt.Sprintf("amendment %d", i+1))
		}
	}
	if len(badVersion) > 0 {
		add("every amendment must have a version in the form MAJOR.MINOR.PATCH, and %s does not", strings.Join(badVersion, ", "))
	}
	if len(badDate) > 0 {
		add("every amendment must have a date in the form YYYY-MM-DD, and %s does not", strings.Join(badDate, ", "))
	}
	if len(noChange) > 0 {
		add("every amendment must have a change saying what changed, and %s has none", strings.Join(noChange, ", "))
	}
	if len(badVersion) == 0 {
		for i := 1; i < len(fm.Amendments); i++ {
			if CompareVersions(fm.Amendments[i].Version, fm.Amendments[i-1].Version) >= 0 {
				add("the amendments' versions must strictly decrease, newest first, and amendment %d (%s) does not come below amendment %d (%s)",
					i+1, fm.Amendments[i].Version, i, fm.Amendments[i-1].Version)
				break
			}
		}
	}
	if len(badDate) == 0 {
		for i := 1; i < len(fm.Amendments); i++ {
			if fm.Amendments[i].Date > fm.Amendments[i-1].Date {
				add("the amendments' dates must not increase, newest first, and amendment %d (%s) is later than amendment %d (%s)",
					i+1, fm.Amendments[i].Date, i, fm.Amendments[i-1].Date)
				break
			}
		}
	}
	if versionOK && len(badVersion) == 0 && fm.Amendments[0].Version != fm.Version {
		add("the first amendment's version (%s) must be the file's version (%s)", fm.Amendments[0].Version, fm.Version)
	}
	return fm, problems
}
