package cli

import (
	"encoding/json"
	"flag"
	"reflect"
	"strings"
	"testing"
	"time"
)

type schemaSample struct {
	Name   string   `json:"name" description:"the name"`
	Count  int      `json:"count,omitempty" description:"how many"`
	Tags   []string `json:"tags" description:"labels"`
	Inner  *inner   `json:"inner,omitempty" description:"a nested object"`
	Hidden string   `json:"-"`
	secret string
}

type inner struct {
	OK bool `json:"ok" description:"whether it held"`
}

// A struct is described the way encoding/json writes it: properties in
// declaration order, json names, omitempty fields optional, each with its
// description after its type.
func TestSchemaForStruct(t *testing.T) {
	b, err := json.Marshal(SchemaFor(reflect.TypeOf(schemaSample{})))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"object","properties":{` +
		`"name":{"type":"string","description":"the name"},` +
		`"count":{"type":"integer","description":"how many"},` +
		`"tags":{"type":"array","description":"labels","items":{"type":"string"}},` +
		`"inner":{"type":"object","description":"a nested object","properties":{"ok":{"type":"boolean","description":"whether it held"}},"required":["ok"]}},` +
		`"required":["name","tags"]}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

// The flags document types each flag from its value, and leaves out
// --schema and --version.
func TestFlagsDocument(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.Int("task", 0, "a task")
	fs.Bool("quiet", false, "be quiet")
	fs.Float64("budget", 5, "dollars")
	fs.Duration("timeout", time.Minute, "how long")
	fs.String("mode", "a", "a mode")
	fs.Bool("schema", false, "")
	fs.Bool("version", false, "")
	b, err := json.Marshal(FlagsDocument(fs, map[string][]string{"mode": {"a", "b"}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"budget":{"type":"number","default":5,"description":"dollars"}`,
		`"mode":{"type":"string","enum":["a","b"],"default":"a","description":"a mode"}`,
		`"quiet":{"type":"boolean","default":false,"description":"be quiet"}`,
		`"task":{"type":"integer","default":0,"description":"a task"}`,
		`"timeout":{"type":"string","format":"duration","default":"1m0s","description":"how long"}`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("lacks %s\n%s", want, b)
		}
	}
	if strings.Contains(string(b), `"schema"`) || strings.Contains(string(b), `"version"`) {
		t.Errorf("lists --schema or --version: %s", b)
	}
}

// The envelope document puts the command's own result in place of result
// and drops the fields the command never writes.
func TestEnvelopeDocument(t *testing.T) {
	doc := EnvelopeDocument(inner{}, "model", "usage")
	props, _ := doc.Get("properties")
	po := props.(SchemaObject)
	if _, ok := po.Get("model"); ok {
		t.Error("model is still described")
	}
	res, _ := po.Get("result")
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), `"ok":{"type":"boolean"`) || !strings.Contains(string(b), `"the command's own result"`) {
		t.Errorf("result = %s", b)
	}
	req, _ := doc.Get("required")
	for _, k := range req.([]string) {
		if k == "model" || k == "usage" {
			t.Errorf("%s is required", k)
		}
	}
}
