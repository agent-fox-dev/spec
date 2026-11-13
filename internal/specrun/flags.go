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
	max            int // 0: no limit
	stall          time.Duration
	logDir         string
	logDirFrom     string // what gave logDir: --log-dir, or its variable
	strictSkills   bool
	strictHandoffs bool
	stopOn         []string
	stopOnText     string // --stop-on as given, or its default
	// reconcileEvery is the number of packages between reconciles; 0 leaves
	// only the reconcile at the end.
	reconcileEvery int
	repair         bool
	repairOn       []string
	repairOnText   string
	repairMax      int
	repairSecurity bool
	dryRun         bool
	ledger         bool
	run            string // under --ledger, the run whose steps it prints
	resetState     bool
	version        bool
}

const usageText = `specrun — run /af-impl on every open spec package, one fresh claude -p session each

Usage:
  specrun [--max N] [--stall D] [--log-dir DIR] [--strict-skills] [--strict-handoffs]
  specrun --dry-run      say what the next step would be, and change nothing
  specrun --ledger [--run ID]
                         print the ledger of every step, or of one run's, as a table
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
func parseOptions(argv []string, lookup func(string) (string, bool)) (*options, error) {
	o := &options{stall: DefaultStall, stopOnText: defaultStopOn, reconcileEvery: defaultReconcileEvery,
		repairOnText: defaultRepairOn, repairMax: defaultRepairMax}
	if err := o.fromEnv(lookup); err != nil {
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
	if _, ok := lookup(envName("stop-on")); !set["stop-on"] && ok {
		from = envName("stop-on")
	}
	stopOn, err := parseSeverities(o.stopOnText, from)
	if err != nil {
		return nil, err
	}
	o.stopOn = stopOn
	from = "--repair-on"
	if _, ok := lookup(envName("repair-on")); !set["repair-on"] && ok {
		from = envName("repair-on")
	}
	if o.repairOn, err = parseRepairOn(o.repairOnText, from); err != nil {
		return nil, err
	}
	if set["repair-max"] && o.repairMax < 0 {
		return nil, fmt.Errorf("--repair-max %d: the number of repairs per reconcile must be 0 or more", o.repairMax)
	}
	o.logDirFrom = "--log-dir"
	if !set["log-dir"] && o.logDir != "" {
		o.logDirFrom = envName("log-dir")
	}
	var errs []error
	if set["max"] && o.max <= 0 {
		errs = append(errs, fmt.Errorf("--max %d: the number of packages to land must be greater than zero", o.max))
	}
	if set["reconcile-every"] && o.reconcileEvery < 0 {
		errs = append(errs, fmt.Errorf("--reconcile-every %d: the number of packages between reconciles must be 0 or more", o.reconcileEvery))
	}
	if set["stall"] && o.stall <= 0 {
		errs = append(errs, fmt.Errorf("--stall %s: a session's stall time must be greater than zero", o.stall))
	}
	if set["run"] && (!o.ledger || o.run == "") {
		errs = append(errs, fmt.Errorf("--run %q: names the run whose steps --ledger prints, and goes with --ledger only", o.run))
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
func (o *options) fromEnv(lookup func(string) (string, bool)) error {
	getenv := func(k string) string { v, _ := lookup(k); return v }
	var errs []error
	if v := getenv(envName("max")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("%s=%s: the number of packages to land must be a whole number greater than zero", envName("max"), v))
		}
		o.max = n
	}
	if v, ok := lookup(envName("repair-on")); ok {
		o.repairOnText = v
	}
	if v := getenv(envName("repair-max")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			errs = append(errs, fmt.Errorf("%s=%s: the number of repairs per reconcile must be a whole number, 0 or more", envName("repair-max"), v))
		}
		o.repairMax = n
	}
	for _, b := range []struct {
		name string
		to   *bool
	}{{"repair", &o.repair}, {"repair-security", &o.repairSecurity}, {"strict-handoffs", &o.strictHandoffs}} {
		if v := getenv(envName(b.name)); v != "" {
			x, err := strconv.ParseBool(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s=%s: must be true or false", envName(b.name), v))
			}
			*b.to = x
		}
	}
	if v := getenv(envName("reconcile-every")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			errs = append(errs, fmt.Errorf("%s=%s: the number of packages between reconciles must be a whole number, 0 or more", envName("reconcile-every"), v))
		}
		o.reconcileEvery = n
	}
	if v := getenv(envName("stall")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s=%s: the stall time must be a duration greater than zero, such as 30m", envName("stall"), v))
		}
		o.stall = d
	}
	o.logDir = getenv(envName("log-dir"))
	// Set to "", the variable stops on nothing: it is not unset.
	if v, ok := lookup(envName("stop-on")); ok {
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
	fs.BoolVar(&o.repair, "repair", o.repair, "after a package that left an open defect of a severity in --stop-on, repair each instead of stopping ($SPECRUN_REPAIR)")
	fs.StringVar(&o.repairOnText, "repair-on", o.repairOnText, "what a reconcile queues for repair, comma-separated: security, data-loss, correctness, minor (defects of that severity), untested; '' none ($SPECRUN_REPAIR_ON)")
	fs.IntVar(&o.repairMax, "repair-max", o.repairMax, "the most repairs after one reconcile, 0 or more; 0 repairs nothing there ($SPECRUN_REPAIR_MAX)")
	fs.BoolVar(&o.repairSecurity, "repair-security", o.repairSecurity, "repair security defects too; without it a security defect is never repaired ($SPECRUN_REPAIR_SECURITY)")
	fs.IntVar(&o.reconcileEvery, "reconcile-every", o.reconcileEvery, "run a reconcile after this many landed packages, 0 or more; 0 leaves only the reconcile at the end ($SPECRUN_RECONCILE_EVERY)")
	fs.StringVar(&o.stopOnText, "stop-on", o.stopOnText, "stop the loop after a package that opened an open defect of one of these severities, comma-separated: security, data-loss, correctness, minor; '' never stops ($SPECRUN_STOP_ON)")
	fs.BoolVar(&o.strictHandoffs, "strict-handoffs", o.strictHandoffs, "validate each package before its session with the cross-spec rules, an inbound handoff no task takes an error, and stop when it does not validate ($SPECRUN_STRICT_HANDOFFS)")
	fs.BoolVar(&o.strictSkills, "strict-skills", o.strictSkills, "refuse to start when the installed af-impl skill is not the one specrun was built with ($SPECRUN_STRICT_SKILLS)")
	fs.BoolVar(&o.dryRun, "dry-run", false, "say what the next step would be and change nothing: no branch switched, nothing pulled, no session, no state or log")
	fs.BoolVar(&o.ledger, "ledger", false, "print the ledger as a table and start nothing")
	fs.StringVar(&o.run, "run", "", "with --ledger, print only the steps of this run ID")
	fs.BoolVar(&o.resetState, "reset-state", false, "discard the state file and do nothing else")
	fs.BoolVar(&o.version, "version", false, "print the build identity and exit")
}

// printUsage writes the help, with every flag and its default, to w.
func printUsage(w io.Writer) {
	fmt.Fprint(w, usageText)
	fs := flag.NewFlagSet("specrun", flag.ContinueOnError)
	fs.SetOutput(w)
	(&options{stall: DefaultStall, stopOnText: defaultStopOn, reconcileEvery: defaultReconcileEvery,
		repairOnText: defaultRepairOn, repairMax: defaultRepairMax}).register(fs)
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
