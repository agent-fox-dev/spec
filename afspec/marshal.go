package afspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// schemaFieldOrder maps Go struct types to their JSON Schema property order.
// This ensures MarshalJSON produces fields in the order the schemas declare them.
var schemaFieldOrder map[reflect.Type][]string

// omitWhenNilFields defines which fields should be omitted when their value is nil.
// Fields NOT listed here are always included (nil values become JSON null).
// In particular:
// - Criterion pattern fields (trigger, condition, etc.) are omitted when nil
// - Schema-optional fields ($schema, external_apis, etc.) are omitted when absent
var omitWhenNilFields map[reflect.Type]map[string]bool

func init() {
	// Field orderings from JSON Schema properties order.
	// These MUST match the order in the corresponding .v3.json schema files.
	schemaFieldOrder = map[reflect.Type][]string{
		// requirements.v3.json
		reflect.TypeOf(RequirementsV3Json{}): {"$schema", "spec_id", "spec_name", "schema_version", "requirements", "execution_paths", "external_apis", "handoffs"},
		reflect.TypeOf(Requirement{}):        {"id", "title", "rationale", "system", "criteria"},
		reflect.TypeOf(Criterion{}):          {"id", "pattern", "condition", "system", "action", "contract", "check"},
		reflect.TypeOf(Check{}):              {"kind", "setup", "pseudocode", "same_as"},
		reflect.TypeOf(PathStep{}):           {"actor", "action"},
		reflect.TypeOf(ExecutionPath{}):      {"id", "title", "steps", "smoke"},
		reflect.TypeOf(Smoke{}):              {"real_components", "setup", "end_state", "pseudocode"},
		reflect.TypeOf(ExternalApiSymbol{}):  {"name", "import_path", "signature", "notes"},
		reflect.TypeOf(ExternalApi{}):        {"package", "version", "verified", "symbols"},
		reflect.TypeOf(Handoff{}):            {"id", "what", "to", "criteria"},

		// tasks.v3.json
		reflect.TypeOf(TasksV3Json{}):  {"$schema", "spec_id", "spec_name", "schema_version", "test_commands", "dependencies", "tasks"},
		reflect.TypeOf(TestCommands{}): {"all_tests", "linter", "spec_tests", "affected_tests"},
		reflect.TypeOf(Dependency{}):   {"spec", "reason"},
		reflect.TypeOf(Task{}):         {"id", "title", "criteria", "steps", "touches", "depends_on", "done_when", "takes_handoffs"},

		// progress.v3.json
		reflect.TypeOf(ProgressV3Json{}): {"$schema", "spec_id", "spec_name", "schema_version", "updated_at", "tasks"},
		reflect.TypeOf(TaskProgress{}):   {"id", "state"},
	}

	// Fields omitted when nil. Everything else is always written, so a nil
	// value would appear as JSON null — which the v3 schemas reject.
	omitWhenNilFields = map[reflect.Type]map[string]bool{
		reflect.TypeOf(Criterion{}): {
			"condition": true,
			"system":    true,
			"contract":  true,
		},
		reflect.TypeOf(Check{}): {
			"kind":       true,
			"setup":      true,
			"pseudocode": true,
			"same_as":    true,
		},
		reflect.TypeOf(Smoke{}): {
			"setup":      true,
			"pseudocode": true,
		},
		reflect.TypeOf(Requirement{}): {
			"rationale": true,
			"system":    true,
		},
		reflect.TypeOf(RequirementsV3Json{}): {
			"execution_paths": true,
			"external_apis":   true,
			"handoffs":        true,
		},
		reflect.TypeOf(ExternalApiSymbol{}): {
			"notes": true,
		},
		reflect.TypeOf(TestCommands{}): {
			"spec_tests":     true,
			"affected_tests": true,
		},
		reflect.TypeOf(Task{}): {
			"touches":        true,
			"depends_on":     true,
			"done_when":      true,
			"takes_handoffs": true,
		},
	}
}

// structFieldInfo holds metadata about a single struct field for serialization.
type structFieldInfo struct {
	Index   []int  // reflect field index
	JsonKey string // JSON key name
	OmitNil bool   // omit when nil (pointer, slice, map, interface)
}

var (
	fieldInfoCache = map[reflect.Type][]structFieldInfo{}
	fieldInfoMu    sync.RWMutex
)

// getStructFields returns the ordered field infos for a struct type,
// using the JSON Schema property order when available.
func getStructFields(t reflect.Type) []structFieldInfo {
	fieldInfoMu.RLock()
	if fields, ok := fieldInfoCache[t]; ok {
		fieldInfoMu.RUnlock()
		return fields
	}
	fieldInfoMu.RUnlock()

	fieldInfoMu.Lock()
	defer fieldInfoMu.Unlock()

	// Double-check after acquiring write lock
	if fields, ok := fieldInfoCache[t]; ok {
		return fields
	}

	// Build field map from struct
	fieldMap := make(map[string]structFieldInfo)
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag := sf.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		jsonKey := parts[0]
		if jsonKey == "" {
			continue
		}

		omitNil := false
		if omitMap, ok := omitWhenNilFields[t]; ok {
			omitNil = omitMap[jsonKey]
		}

		fieldMap[jsonKey] = structFieldInfo{
			Index:   sf.Index,
			JsonKey: jsonKey,
			OmitNil: omitNil,
		}
	}

	// Order fields based on schema order
	order, ok := schemaFieldOrder[t]
	if !ok {
		// Fallback: use struct declaration order
		order = make([]string, 0, len(fieldMap))
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			tag := sf.Tag.Get("json")
			if tag == "" || tag == "-" {
				continue
			}
			parts := strings.Split(tag, ",")
			jsonKey := parts[0]
			if jsonKey != "" {
				order = append(order, jsonKey)
			}
		}
	}

	fields := make([]structFieldInfo, 0, len(order))
	for _, key := range order {
		if fi, ok := fieldMap[key]; ok {
			fields = append(fields, fi)
		}
	}

	fieldInfoCache[t] = fields
	return fields
}

// isNilValue checks whether a reflect.Value is nil (for pointer, interface, slice, map types).
func isNilValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		return v.IsNil()
	default:
		return false
	}
}

// MarshalJSON serializes a spec artifact struct to deterministic JSON.
// Struct fields are serialized in JSON Schema property order and all
// map[string]T keys are sorted alphabetically. The output uses 2-space
// indentation with a trailing newline, so that a spec on disk round-trips
// through LoadSpec and Save byte-for-byte.
func MarshalJSON(v interface{}) ([]byte, error) {
	if v == nil {
		return []byte("null\n"), nil
	}
	w := &jsonWriter{}
	if err := w.writeValue(reflect.ValueOf(v)); err != nil {
		return nil, err
	}
	w.buf.WriteByte('\n')
	return w.buf.Bytes(), nil
}

// jsonWriter is a custom JSON encoder that produces deterministic output
// in the format MarshalJSON documents.
type jsonWriter struct {
	buf   bytes.Buffer
	depth int
	// ext holds the extension keys to write back, by the address of their
	// object, and address is the address of the value being written
	// (extensions.go). Both are empty for MarshalJSON.
	ext     extensionSet
	address string
}

// at writes v as the value at the address under the current one, segment
// appended, and restores the current address after.
func (w *jsonWriter) at(segment string, v reflect.Value) error {
	parent := w.address
	w.address = parent + "/" + segment
	err := w.writeValue(v)
	w.address = parent
	return err
}

func (w *jsonWriter) indent() {
	for i := 0; i < w.depth; i++ {
		w.buf.WriteString("  ")
	}
}

func (w *jsonWriter) writeValue(v reflect.Value) error {
	// Handle invalid (nil interface passed directly)
	if !v.IsValid() {
		w.buf.WriteString("null")
		return nil
	}

	// Dereference interfaces
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			w.buf.WriteString("null")
			return nil
		}
		return w.writeValue(v.Elem())
	}

	// Dereference pointers
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			w.buf.WriteString("null")
			return nil
		}
		return w.writeValue(v.Elem())
	}

	switch v.Kind() {
	case reflect.Struct:
		return w.writeStruct(v)
	case reflect.Map:
		return w.writeMap(v)
	case reflect.Slice:
		return w.writeSlice(v)
	case reflect.String:
		w.writeString(v.String())
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Fprintf(&w.buf, "%d", v.Int())
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		fmt.Fprintf(&w.buf, "%d", v.Uint())
		return nil
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return fmt.Errorf("unsupported float value: %v", f)
		}
		if f == math.Trunc(f) && math.Abs(f) < 1e15 {
			// Whole number — output as an integer, without a decimal point
			fmt.Fprintf(&w.buf, "%d", int64(f))
		} else {
			fmt.Fprintf(&w.buf, "%g", f)
		}
		return nil
	case reflect.Bool:
		if v.Bool() {
			w.buf.WriteString("true")
		} else {
			w.buf.WriteString("false")
		}
		return nil
	default:
		return fmt.Errorf("MarshalJSON: unsupported type %s (kind %s)", v.Type(), v.Kind())
	}
}

func (w *jsonWriter) writeString(s string) {
	// encoding/json's Marshal escapes <, > and & as \u003c, \u003e and \u0026
	// for safe embedding in HTML. Spec text is full of those characters —
	// "<message>", "a && b" — and the escaping would make a spec that contains
	// one fail to round-trip byte-for-byte, so it is turned off here.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		// A Go string always marshals, so this cannot happen in practice.
		w.buf.WriteString(`""`)
		return
	}
	// Encode appends a newline that the caller does not want.
	w.buf.Write(bytes.TrimRight(buf.Bytes(), "\n"))
}

func (w *jsonWriter) writeStruct(v reflect.Value) error {
	fields := getStructFields(v.Type())

	// Collect non-omitted fields
	type fieldEntry struct {
		info  structFieldInfo
		value reflect.Value
	}
	entries := make([]fieldEntry, 0, len(fields))
	for _, fi := range fields {
		fv := v.FieldByIndex(fi.Index)
		if fi.OmitNil && isNilValue(fv) {
			continue
		}
		entries = append(entries, fieldEntry{fi, fv})
	}

	extensions := w.extensionsAt(w.address)
	if len(entries) == 0 && len(extensions) == 0 {
		w.buf.WriteString("{}")
		return nil
	}

	w.buf.WriteByte('{')
	w.depth++
	for i, entry := range entries {
		if i > 0 {
			w.buf.WriteByte(',')
		}
		w.buf.WriteByte('\n')
		w.indent()
		w.writeString(entry.info.JsonKey)
		w.buf.WriteString(": ")
		if err := w.at(entry.info.JsonKey, entry.value); err != nil {
			return err
		}
	}
	// The object's extension keys follow its own fields, in key order.
	for i, key := range extensions {
		if i > 0 || len(entries) > 0 {
			w.buf.WriteByte(',')
		}
		w.buf.WriteByte('\n')
		w.indent()
		w.writeString(key)
		w.buf.WriteString(": ")
		if err := w.at(key, reflect.ValueOf(w.ext[w.address][key])); err != nil {
			return err
		}
	}
	w.depth--
	w.buf.WriteByte('\n')
	w.indent()
	w.buf.WriteByte('}')
	return nil
}

func (w *jsonWriter) writeMap(v reflect.Value) error {
	if v.IsNil() {
		w.buf.WriteString("null")
		return nil
	}

	keys := v.MapKeys()
	if len(keys) == 0 {
		w.buf.WriteString("{}")
		return nil
	}

	// Sort keys alphabetically
	sortedKeys := make([]string, len(keys))
	for i, k := range keys {
		sortedKeys[i] = k.String()
	}
	sort.Strings(sortedKeys)

	w.buf.WriteByte('{')
	w.depth++
	for i, key := range sortedKeys {
		if i > 0 {
			w.buf.WriteByte(',')
		}
		w.buf.WriteByte('\n')
		w.indent()
		w.writeString(key)
		w.buf.WriteString(": ")
		if err := w.at(key, v.MapIndex(reflect.ValueOf(key))); err != nil {
			return err
		}
	}
	w.depth--
	w.buf.WriteByte('\n')
	w.indent()
	w.buf.WriteByte('}')
	return nil
}

func (w *jsonWriter) writeSlice(v reflect.Value) error {
	if v.IsNil() {
		w.buf.WriteString("null")
		return nil
	}

	if v.Len() == 0 {
		w.buf.WriteString("[]")
		return nil
	}

	w.buf.WriteByte('[')
	w.depth++
	for i := 0; i < v.Len(); i++ {
		if i > 0 {
			w.buf.WriteByte(',')
		}
		w.buf.WriteByte('\n')
		w.indent()
		if err := w.at(valueSegment(v.Index(i), i), v.Index(i)); err != nil {
			return err
		}
	}
	w.depth--
	w.buf.WriteByte('\n')
	w.indent()
	w.buf.WriteByte(']')
	return nil
}
