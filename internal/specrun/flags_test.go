package specrun

import (
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
			o, err := parseOptions(tc.argv, func(k string) string { return tc.env[k] })
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
