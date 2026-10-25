package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The document lands whole, under a parent created on the way, as an ordinary
// file, with no temporary file left beside it; a path that cannot be written
// is an error and leaves nothing.
func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "env.json")
	if err := WriteAtomic(path, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != `{"ok":true}` {
		t.Errorf("file = %q, %v", b, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, %v; want 0644", info.Mode().Perm(), err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("the directory holds %d entries; want the document alone", len(entries))
	}

	unwritable := unwritableOutput(t)
	if err := WriteAtomic(unwritable, []byte("x")); err == nil {
		t.Error("writing under a file succeeded")
	}
	if _, err := os.Stat(unwritable); err == nil {
		t.Error("a file exists at the unwritable path")
	}
}

// A result that cannot be encoded still yields one envelope on stdout, an emit
// failure with exit 1; when that fallback cannot be written to --output
// either, no warning is added, since no further envelope is left to carry it.
func TestEmitWithOutput_FallsBackWhenTheResultCannotBeEncoded(t *testing.T) {
	unwritable := unwritableOutput(t)
	env := NewRun("tool", "test").Envelope(ExitOK, map[string]any{"bad": make(chan int)}, nil)
	var buf bytes.Buffer
	if code := EmitWithOutput(&buf, unwritable, env); code != ExitFailed {
		t.Errorf("code = %d, want %d", code, ExitFailed)
	}
	var got Envelope
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not the fallback envelope: %v\n%s", err, buf.String())
	}
	if got.OK || got.ExitCode != ExitFailed || got.Error == nil || got.Error.Stage != "emit" {
		t.Errorf("stdout is not the encoding-failure fallback: %+v", got)
	}
	if len(got.Warnings) != 0 {
		t.Errorf("fallback carries warnings %+v, want none", got.Warnings)
	}
	if _, err := os.Stat(unwritable); err == nil {
		t.Error("a file exists at the unwritable path")
	}
}

// What a command writes is read by programs and models, never embedded in
// HTML: <, > and & are written as they are.
func TestEncodeJSON_DoesNotEscapeHTML(t *testing.T) {
	b, err := EncodeJSON(map[string]string{"md": "a < b && c > d"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"md\": \"a < b && c > d\"\n}\n"; string(b) != want {
		t.Errorf("EncodeJSON = %q, want %q", b, want)
	}
}
