package specrun

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Whatever a session prints, stdout holds one envelope, and stderr one line
// per step.
func TestMain_WritesOneEnvelopeAndOneLinePerStep(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.plan(t, map[string]any{"events": 200, "actions": []string{"land"}, "result": "done"})
	var stdout, stderr bytes.Buffer
	if code := Main(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr.String())
	}
	dec := json.NewDecoder(&stdout)
	var env map[string]any
	if err := dec.Decode(&env); err != nil || env["tool"] != "specrun" || dec.More() {
		t.Fatalf("stdout is not one specrun envelope: %v %v", err, env)
	}
	var steps []string
	for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
		if strings.Contains(line, "implement-01_a") || strings.Contains(line, "implement 01_a") {
			steps = append(steps, line)
		}
	}
	if len(steps) != 1 {
		t.Errorf("stderr names the step on %d lines; want one:\n%s", len(steps), stderr.String())
	}
}
