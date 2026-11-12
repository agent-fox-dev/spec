package specrun

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DefaultStall is how long a session may write nothing before it is ended.
const DefaultStall = 30 * time.Minute

// options are specrun's settings. A setting's default comes from its SPECRUN_
// variable when that is set, and the flag wins over it.
type options struct {
	max          int // 0: no limit
	stall        time.Duration
	logDir       string
	logDirFrom   string // what gave logDir: --log-dir, or its variable
	strictSkills bool
	stopOn       []string
	stopOnText   string // --stop-on as given, or its default
	dryRun       bool
	ledger       bool
	resetState   bool
	version      bool
}

const usageText = `specrun — run /af-impl on every open spec package, one fresh claude -p session each

Usage:
  specrun [--max N] [--stall D] [--log-dir DIR] [--strict-skills]
  specrun --dry-run      say what the next step would be, and change nothing
  specrun --ledger       print the ledger of every step as a table
  specrun --reset-state  discard the state file
  specrun --version

Each flag's default comes from SPECRUN_<FLAG> (SPECRUN_MAX, SPECRUN_STALL,
SPECRUN_LOG_DIR, SPECRUN_STRICT_SKILLS); the flag wins. Output is one JSON
envelope on stdout; progress goes to stderr. See docs/specrun.md.

Flags:
`

// envName is the variable that sets the default of the flag name.
func envName(name string) string {
	return "SPECRUN_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// parseOptions reads the settings from argv and the environment. Its error is
// a usage error; flag.ErrHelp asks for the help.
func parseOptions(argv []string, getenv func(string) string) (*options, error) {
	o := &options{stall: DefaultStall, stopOnText: defaultStopOn}
	if err := o.fromEnv(getenv); err != nil {
		return nil, err
	}
	fs := flag.NewFlagSet("specrun", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o.register(fs)
	fs.Usage = func() {}
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, err
		}
		// The flag package names a flag with one dash; the help, two.
		return nil, errors.New(strings.NewReplacer("flag -", "flag --", "defined: -", "defined: --").Replace(err.Error()))
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("specrun takes no argument, got %q: it chooses the packages itself", fs.Arg(0))
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	from := "--stop-on"
	if !set["stop-on"] && getenv(envName("stop-on")) != "" {
		from = envName("stop-on")
	}
	stopOn, err := parseSeverities(o.stopOnText, from)
	if err != nil {
		return nil, err
	}
	o.stopOn = stopOn
	o.logDirFrom = "--log-dir"
	if !set["log-dir"] && o.logDir != "" {
		o.logDirFrom = envName("log-dir")
	}
	var errs []error
	if set["max"] && o.max <= 0 {
		errs = append(errs, fmt.Errorf("--max %d: the number of packages to land must be greater than zero", o.max))
	}
	if set["stall"] && o.stall <= 0 {
		errs = append(errs, fmt.Errorf("--stall %s: a session's stall time must be greater than zero", o.stall))
	}
	ops := []struct {
		name  string
		given bool
	}{{"--dry-run", o.dryRun}, {"--ledger", o.ledger}, {"--reset-state", o.resetState}}
	for i := range ops {
		for j := i + 1; j < len(ops); j++ {
			if ops[i].given && ops[j].given {
				errs = append(errs, fmt.Errorf("%s and %s are two operations: give one", ops[i].name, ops[j].name))
			}
		}
	}
	return o, errors.Join(errs...)
}

// fromEnv takes the defaults from the SPECRUN_ variables that are set. A
// value its flag would refuse is a usage error naming the variable.
func (o *options) fromEnv(getenv func(string) string) error {
	var errs []error
	if v := getenv(envName("max")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("%s=%s: the number of packages to land must be a whole number greater than zero", envName("max"), v))
		}
		o.max = n
	}
	if v := getenv(envName("stall")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s=%s: the stall time must be a duration greater than zero, such as 30m", envName("stall"), v))
		}
		o.stall = d
	}
	o.logDir = getenv(envName("log-dir"))
	if v := getenv(envName("stop-on")); v != "" {
		o.stopOnText = v
	}
	if v := getenv(envName("strict-skills")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%s: must be true or false", envName("strict-skills"), v))
		}
		o.strictSkills = b
	}
	return errors.Join(errs...)
}

// register defines the flags on fs, each defaulting to what o holds.
func (o *options) register(fs *flag.FlagSet) {
	fs.IntVar(&o.max, "max", o.max, "land at most this many packages in this run, a whole number greater than zero; default no limit ($SPECRUN_MAX)")
	fs.DurationVar(&o.stall, "stall", o.stall, "end a session that writes no event for this long, a duration greater than zero ($SPECRUN_STALL)")
	fs.StringVar(&o.logDir, "log-dir", o.logDir, "where the sessions' streams, final messages and the ledger go; default agent-fox/specrun/<repository> under the user's cache directory; never inside the working tree ($SPECRUN_LOG_DIR)")
	fs.StringVar(&o.stopOnText, "stop-on", o.stopOnText, "stop the loop after a package that opened an open defect of one of these severities, comma-separated: security, data-loss, correctness, minor; '' never stops ($SPECRUN_STOP_ON)")
	fs.BoolVar(&o.strictSkills, "strict-skills", o.strictSkills, "refuse to start when the installed af-impl skill is not the one specrun was built with ($SPECRUN_STRICT_SKILLS)")
	fs.BoolVar(&o.dryRun, "dry-run", false, "say what the next step would be and change nothing: no branch switched, nothing pulled, no session, no state or log")
	fs.BoolVar(&o.ledger, "ledger", false, "print the ledger as a table and start nothing")
	fs.BoolVar(&o.resetState, "reset-state", false, "discard the state file and do nothing else")
	fs.BoolVar(&o.version, "version", false, "print the build identity and exit")
}

// printUsage writes the help, with every flag and its default, to w.
func printUsage(w io.Writer) {
	fmt.Fprint(w, usageText)
	fs := flag.NewFlagSet("specrun", flag.ContinueOnError)
	fs.SetOutput(w)
	(&options{stall: DefaultStall}).register(fs)
	fs.PrintDefaults()
}

// The severities of a defect, and those the loop stops on by default: a lost
// write or a leak is what a later package silently builds on.
var severities = []string{"security", "data-loss", "correctness", "minor"}

const defaultStopOn = "security,data-loss"

// parseSeverities reads a comma-separated list of severities; the empty
// string is none. from names where the value came from.
func parseSeverities(v, from string) ([]string, error) {
	out := []string{}
	if v == "" {
		return out, nil
	}
	for _, s := range strings.Split(v, ",") {
		if !slices.Contains(severities, s) {
			return nil, fmt.Errorf("%s %q: %q is not a severity; the severities are %s, comma-separated, and '' stops on none",
				from, v, s, strings.Join(severities, ", "))
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out, nil
}
