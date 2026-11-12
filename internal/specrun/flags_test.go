package specrun

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Each setting is a flag whose default an SPECRUN_ variable sets, the flag
// winning over the variable.
func TestOptions_TakeTheirDefaultsFromTheEnvironment(t *testing.T) {
	for name, tc := range map[string]struct {
		argv  []string
		env   map[string]string
		check func(o *options) bool
	}{
		"max, neither":     {nil, nil, func(o *options) bool { return o.max == 0 }},
		"max, variable":    {nil, map[string]string{"SPECRUN_MAX": "3"}, func(o *options) bool { return o.max == 3 }},
		"max, both":        {[]string{"--max", "2"}, map[string]string{"SPECRUN_MAX": "3"}, func(o *options) bool { return o.max == 2 }},
		"stall, neither":   {nil, nil, func(o *options) bool { return o.stall == 30*time.Minute }},
		"stall, variable":  {nil, map[string]string{"SPECRUN_STALL": "5m"}, func(o *options) bool { return o.stall == 5*time.Minute }},
		"stall, both":      {[]string{"--stall", "90s"}, map[string]string{"SPECRUN_STALL": "5m"}, func(o *options) bool { return o.stall == 90*time.Second }},
		"log dir, neither": {nil, nil, func(o *options) bool { return o.logDir == "" }},
		"log dir, variable": {nil, map[string]string{"SPECRUN_LOG_DIR": "/var/a"},
			func(o *options) bool { return o.logDir == "/var/a" }},
		"log dir, both": {[]string{"--log-dir", "/var/b"}, map[string]string{"SPECRUN_LOG_DIR": "/var/a"},
			func(o *options) bool { return o.logDir == "/var/b" }},
		"strict skills, neither": {nil, nil, func(o *options) bool { return !o.strictSkills }},
		"strict skills, variable": {nil, map[string]string{"SPECRUN_STRICT_SKILLS": "true"},
			func(o *options) bool { return o.strictSkills }},
		"strict skills, both": {[]string{"--strict-skills=false"}, map[string]string{"SPECRUN_STRICT_SKILLS": "1"},
			func(o *options) bool { return !o.strictSkills }},
	} {
		t.Run(name, func(t *testing.T) {
			o, err := parseOptions(tc.argv, envOf(tc.env))
			if err != nil || !tc.check(o) {
				t.Errorf("parseOptions(%v, %v) = %+v, %v", tc.argv, tc.env, o, err)
			}
		})
	}
}

// A call that cannot hold is a usage error, exit code 2, naming what is wrong.
func TestMain_RefusesACallThatCannotHold(t *testing.T) {
	for name, tc := range map[string]struct {
		argv []string
		env  map[string]string
		says string
	}{
		"an unknown flag":             {[]string{"--frobnicate"}, nil, "frobnicate"},
		"a positional argument":       {[]string{"19"}, nil, "19"},
		"--dry-run and --ledger":      {[]string{"--dry-run", "--ledger"}, nil, "--ledger"},
		"--ledger and --reset-state":  {[]string{"--ledger", "--reset-state"}, nil, "--reset-state"},
		"--dry-run and --reset-state": {[]string{"--dry-run", "--reset-state"}, nil, "--dry-run"},
		"--max 0":                     {[]string{"--max", "0"}, nil, "--max"},
		"--max -1":                    {[]string{"--max", "-1"}, nil, "--max"},
		"--stall 0s":                  {[]string{"--stall", "0s"}, nil, "--stall"},
		"--stall -1m":                 {[]string{"--stall", "-1m"}, nil, "--stall"},
		"--stall soon":                {[]string{"--stall", "soon"}, nil, "--stall"},
		"SPECRUN_STALL=soon":          {nil, map[string]string{"SPECRUN_STALL": "soon"}, "SPECRUN_STALL"},
		"SPECRUN_MAX=x":               {nil, map[string]string{"SPECRUN_MAX": "x"}, "SPECRUN_MAX"},
		"SPECRUN_MAX=0":               {nil, map[string]string{"SPECRUN_MAX": "0"}, "SPECRUN_MAX"},
		"SPECRUN_STRICT_SKILLS=maybe": {nil, map[string]string{"SPECRUN_STRICT_SKILLS": "maybe"}, "SPECRUN_STRICT_SKILLS"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			env, code, _ := runMain(t, tc.argv...)
			if code != 2 || env.OK || env.Error == nil || env.Error.Category != "usage" || !strings.Contains(env.Error.Message, tc.says) {
				t.Errorf("code %d, error %+v; want a usage error saying %q", code, env.Error, tc.says)
			}
			if len(f.starts(t)) != 0 {
				t.Error("a session was started")
			}
		})
	}
}

// A log directory inside the working tree is refused, since af-impl refuses a
// dirty tree, and one that cannot be written stops the run; without the flag
// the directory is under the user's cache directory, named for the repository.
func TestMain_RefusesALogDirectoryItCannotUse(t *testing.T) {
	for name, tc := range map[string]struct {
		dir  func(f *fixture) string
		code int
		says string
	}{
		"inside the working tree": {func(f *fixture) string { return filepath.Join(f.root, "logs") }, 2, "dirty"},
		"under a regular file": {func(f *fixture) string {
			file := filepath.Join(f.scratch, "file")
			writeFile(t, file, "")
			return filepath.Join(file, "logs")
		}, 1, "log"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.plan(t, land(0))
			env, code, _ := runMain(t, "--log-dir", tc.dir(f))
			if code != tc.code || env.Error == nil || !strings.Contains(env.Error.Message, tc.says) {
				t.Errorf("code %d, error %+v; want exit %d saying %q", code, env.Error, tc.code, tc.says)
			}
			if tc.code == 1 && env.Error != nil && env.Error.Stage != "preflight" {
				t.Errorf("stage %q; want preflight", env.Error.Stage)
			}
			if len(f.starts(t)) != 0 {
				t.Error("a session was started")
			}
		})
	}
	t.Run("the default", func(t *testing.T) {
		f := newFixture(t)
		f.addPackage(t, "01_a", "01", "pending")
		f.plan(t, land(0))
		if _, code, _ := runMain(t); code != 0 {
			t.Fatalf("code %d", code)
		}
		if !exists(filepath.Join(f.cache, "agent-fox", "specrun", filepath.Base(f.root), "ledger.jsonl")) {
			t.Error("no ledger under $XDG_CACHE_HOME/agent-fox/specrun/<repository>")
		}
	})
}

// A log directory the variable gives is reported by the variable's name.
func TestMain_NamesTheVariableOfALogDirectoryItRefuses(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.plan(t, land(0))
	t.Setenv("SPECRUN_LOG_DIR", filepath.Join(f.root, "logs"))
	env, code, _ := runMain(t)
	if code != 2 || env.Error == nil || !strings.Contains(env.Error.Message, "SPECRUN_LOG_DIR") {
		t.Errorf("code %d, error %+v; want a usage error naming SPECRUN_LOG_DIR", code, env.Error)
	}
}

// --stop-on is a comma-separated list of the four severities, the empty
// string meaning never stop, SPECRUN_STOP_ON its default.
func TestOptions_TakeTheSeveritiesToStopOn(t *testing.T) {
	for name, tc := range map[string]struct {
		argv []string
		env  map[string]string
		want []string
		err  string
	}{
		"absent":                     {nil, nil, []string{"security", "data-loss"}, ""},
		"minor":                      {[]string{"--stop-on", "minor"}, nil, []string{"minor"}, ""},
		"two":                        {[]string{"--stop-on", "security,correctness"}, nil, []string{"security", "correctness"}, ""},
		"empty":                      {[]string{"--stop-on", ""}, nil, []string{}, ""},
		"the variable":               {nil, map[string]string{"SPECRUN_STOP_ON": "minor"}, []string{"minor"}, ""},
		"the flag over the variable": {[]string{"--stop-on", "security"}, map[string]string{"SPECRUN_STOP_ON": "minor"}, []string{"security"}, ""},
		"an unknown severity":        {[]string{"--stop-on", "severe"}, nil, nil, "severe"},
		"an empty element":           {[]string{"--stop-on", "security,"}, nil, nil, "--stop-on"},
		"a bad variable":             {nil, map[string]string{"SPECRUN_STOP_ON": "severe"}, nil, "SPECRUN_STOP_ON"},
	} {
		t.Run(name, func(t *testing.T) {
			o, err := parseOptions(tc.argv, envOf(tc.env))
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) || !strings.Contains(err.Error(), "data-loss") {
					t.Errorf("err %v; want one naming %q and the four severities", err, tc.err)
				}
				return
			}
			if err != nil || strings.Join(o.stopOn, ",") != strings.Join(tc.want, ",") || o.stopOn == nil {
				t.Errorf("stopOn %#v, %v; want %v", o, err, tc.want)
			}
		})
	}
}

// envOf is a lookup over the variables in m, a variable set to "" being set.
func envOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// Through Main, a severity outside the four is a usage error, and an empty
// --stop-on lets a package with a security defect go on.
func TestMain_TakesStopOnFromTheCall(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	f.forge.add("01_a: leak", "open", "af:defect", "af:severity-security", "af:from-01")
	f.plan(t, land(0))
	if env, code, _ := runMain(t, "--stop-on", "severe"); code != 2 || env.Error == nil || !strings.Contains(env.Error.Message, "severe") {
		t.Errorf("--stop-on severe: code %d, error %+v; want a usage error naming it", code, env.Error)
	}
	if _, code, stderr := runMain(t, "--stop-on", ""); code != 0 || len(specDirs(f.starts(t))) != 2 {
		t.Errorf("--stop-on '': code %d, sessions %v; want both packages\n%s", code, specDirs(f.starts(t)), stderr)
	}
}

// --reconcile-every is a whole number, 0 meaning only the reconcile at the
// end, SPECRUN_RECONCILE_EVERY its default.
func TestOptions_TakeTheReconcileInterval(t *testing.T) {
	for name, tc := range map[string]struct {
		argv []string
		env  map[string]string
		want int
		err  string
	}{
		"absent":         {nil, nil, 5, ""},
		"3":              {[]string{"--reconcile-every", "3"}, nil, 3, ""},
		"0":              {[]string{"--reconcile-every", "0"}, nil, 0, ""},
		"the variable":   {nil, map[string]string{"SPECRUN_RECONCILE_EVERY": "2"}, 2, ""},
		"-1":             {[]string{"--reconcile-every", "-1"}, nil, 0, "--reconcile-every"},
		"often":          {[]string{"--reconcile-every", "often"}, nil, 0, "reconcile-every"},
		"a bad variable": {nil, map[string]string{"SPECRUN_RECONCILE_EVERY": "often"}, 0, "SPECRUN_RECONCILE_EVERY"},
	} {
		t.Run(name, func(t *testing.T) {
			o, err := parseOptions(tc.argv, envOf(tc.env))
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Errorf("err %v; want one naming %s", err, tc.err)
				}
				return
			}
			if err != nil || o.reconcileEvery != tc.want {
				t.Errorf("reconcileEvery %+v, %v; want %d", o, err, tc.want)
			}
		})
	}
}

// Through Main a bad --reconcile-every is a usage error, exit code 2.
func TestMain_RefusesABadReconcileInterval(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	for _, v := range []string{"-1", "often"} {
		if env, code, _ := runMain(t, "--reconcile-every", v); code != 2 || env.Error == nil || !strings.Contains(env.Error.Message, "reconcile-every") {
			t.Errorf("--reconcile-every %s: code %d, error %+v", v, code, env.Error)
		}
	}
	if len(f.starts(t)) != 0 {
		t.Error("a session started")
	}
}

// --repair, --repair-on, --repair-max and --repair-security, each with its
// SPECRUN_ variable.
func TestOptions_TakeTheRepairSettings(t *testing.T) {
	for name, tc := range map[string]struct {
		argv  []string
		env   map[string]string
		check func(o *options) bool
		err   string
	}{
		"repair-on absent": {nil, nil, func(o *options) bool {
			return strings.Join(o.repairOn, ",") == "security,data-loss,correctness,minor,untested"
		}, ""},
		"repair-on two":           {[]string{"--repair-on", "minor,untested"}, nil, func(o *options) bool { return strings.Join(o.repairOn, ",") == "minor,untested" }, ""},
		"repair-on empty":         {[]string{"--repair-on", ""}, nil, func(o *options) bool { return o.repairOn != nil && len(o.repairOn) == 0 }, ""},
		"repair-on bad":           {[]string{"--repair-on", "tests"}, nil, nil, `--repair-on "tests"`},
		"SPECRUN_REPAIR_ON bad":   {nil, map[string]string{"SPECRUN_REPAIR_ON": "tests"}, nil, `SPECRUN_REPAIR_ON "tests"`},
		"SPECRUN_REPAIR_ON empty": {nil, map[string]string{"SPECRUN_REPAIR_ON": ""}, func(o *options) bool { return o.repairOn != nil && len(o.repairOn) == 0 }, ""},
		"repair-max absent":       {nil, nil, func(o *options) bool { return o.repairMax == 5 }, ""},
		"repair-max 0":            {[]string{"--repair-max", "0"}, nil, func(o *options) bool { return o.repairMax == 0 }, ""},
		"repair-max -1":           {[]string{"--repair-max", "-1"}, nil, nil, "--repair-max"},
		"SPECRUN_REPAIR_MAX":      {nil, map[string]string{"SPECRUN_REPAIR_MAX": "2"}, func(o *options) bool { return o.repairMax == 2 }, ""},
		"SPECRUN_REPAIR_MAX bad":  {nil, map[string]string{"SPECRUN_REPAIR_MAX": "x"}, nil, "SPECRUN_REPAIR_MAX"},
		"SPECRUN_REPAIR_SECURITY": {nil, map[string]string{"SPECRUN_REPAIR_SECURITY": "true"}, func(o *options) bool { return o.repairSecurity }, ""},
		"SPECRUN_REPAIR":          {nil, map[string]string{"SPECRUN_REPAIR": "1"}, func(o *options) bool { return o.repair }, ""},
	} {
		t.Run(name, func(t *testing.T) {
			o, err := parseOptions(tc.argv, envOf(tc.env))
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Errorf("err %v; want one naming %s", err, tc.err)
				}
				return
			}
			if err != nil || !tc.check(o) {
				t.Errorf("options %+v, %v", o, err)
			}
		})
	}
}
