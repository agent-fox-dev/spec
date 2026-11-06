package afspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Extension keys (format v2 §10.1): every object of the four schemas accepts a
// key that begins with "x-", with any value, for a team's own metadata. The
// artifact types are generated from the schemas and have no field for one, so
// encoding/json drops it. LoadSpec records each such key with the address of
// the object it was on, and every save writes it back there, after the
// object's own fields, in key order. An extension key is not an unknown key.

// ExtensionPrefix begins every extension key.
const ExtensionPrefix = "x-"

// IsExtensionKey reports whether key is an extension key.
func IsExtensionKey(key string) bool { return strings.HasPrefix(key, ExtensionPrefix) }

// extensionSet holds one file's extension keys: for each object's address,
// its keys and their decoded values.
//
// An address is the object's path from the root of the file: "" for the root,
// "/<key>" for the object under a field, and, for an element of an array,
// "/id=<id>" when the element has an id (a requirement, a criterion, an
// execution path, a test, a task) and "/<index>" otherwise. The library's
// mutations add and remove objects that have ids, so an id survives them where
// a position would not.
type extensionSet map[string]map[string]any

// collectExtensions records in out the extension keys of raw, a decoded JSON
// value, walking it against t, the Go type it decodes into, as UnknownFields
// does.
func collectExtensions(raw any, t reflect.Type, address string, out extensionSet) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(unmarshalerType) {
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := raw.(map[string]any)
		if !ok {
			return
		}
		fields := jsonFields(t)
		for k, v := range obj {
			if IsExtensionKey(k) {
				if out[address] == nil {
					out[address] = map[string]any{}
				}
				out[address][k] = v
				continue
			}
			if ft, known := fields[k]; known {
				collectExtensions(v, ft, address+"/"+k, out)
			}
		}
	case reflect.Slice, reflect.Array:
		arr, ok := raw.([]any)
		if !ok {
			return
		}
		for i, e := range arr {
			collectExtensions(e, t.Elem(), address+"/"+rawSegment(e, t.Elem(), i), out)
		}
	case reflect.Map:
		obj, ok := raw.(map[string]any)
		if !ok {
			return
		}
		for k, e := range obj {
			collectExtensions(e, t.Elem(), address+"/"+k, out)
		}
	}
}

// rawSegment is the address segment of the array element raw at position i,
// whose Go type is t: its id when t has an id field and raw carries one, its
// position otherwise.
func rawSegment(raw any, t reflect.Type, i int) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		if _, has := jsonFields(t)["id"]; has {
			if obj, ok := raw.(map[string]any); ok {
				switch id := obj["id"].(type) {
				case string:
					return "id=" + id
				case float64:
					return "id=" + strconv.FormatFloat(id, 'f', -1, 64)
				}
			}
		}
	}
	return strconv.Itoa(i)
}

// valueSegment is rawSegment for a Go value: the element's id when its type
// has an id field, its position otherwise.
func valueSegment(v reflect.Value, i int) string {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return strconv.Itoa(i)
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		t := v.Type()
		for j := range t.NumField() {
			name, _, _ := strings.Cut(t.Field(j).Tag.Get("json"), ",")
			if name != "id" {
				continue
			}
			switch f := v.Field(j); f.Kind() {
			case reflect.String:
				return "id=" + f.String()
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				return "id=" + strconv.FormatInt(f.Int(), 10)
			}
		}
	}
	return strconv.Itoa(i)
}

// extensionPaths lists, as JSON pointers, the extension keys of raw walked
// against target's type, so that a caller that must not accept one can name
// it. Nil when there are none.
func extensionPaths(raw any, target any) []string {
	set := extensionSet{}
	collectExtensions(raw, reflect.TypeOf(target), "", set)
	var out []string
	for address, keys := range set {
		for k := range keys {
			out = append(out, address+"/"+k)
		}
	}
	sort.Strings(out)
	return out
}

// marshalArtifact is MarshalJSON with the file's extension keys written back
// on the objects they were loaded from.
func marshalArtifact(v any, ext extensionSet) ([]byte, error) {
	if v == nil {
		return []byte("null\n"), nil
	}
	w := &jsonWriter{ext: ext}
	if err := w.writeValue(reflect.ValueOf(v)); err != nil {
		return nil, err
	}
	w.buf.WriteByte('\n')
	return w.buf.Bytes(), nil
}

// extensionsAt are the extension keys of the object at address, in key
// order, or nil.
func (w *jsonWriter) extensionsAt(address string) []string {
	if w.ext == nil {
		return nil
	}
	keys := make([]string, 0, len(w.ext[address]))
	for k := range w.ext[address] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// frontmatterExtensions are the extension keys of a PRD's YAML frontmatter,
// decoded into a map.
func frontmatterExtensions(fm map[string]any) map[string]any {
	var out map[string]any
	for k, v := range fm {
		if IsExtensionKey(k) {
			if out == nil {
				out = map[string]any{}
			}
			out[k] = v
		}
	}
	return out
}

// renderFrontmatterExtensions writes the frontmatter's extension keys, one a
// line in key order, each value as compact JSON, which is valid YAML: the
// frontmatter renderer writes no value through a YAML marshaller, so that a
// save is byte-stable.
func renderFrontmatterExtensions(b *strings.Builder, ext map[string]any) {
	keys := make([]string, 0, len(ext))
	for k := range ext {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(jsonCompatible(ext[k])); err != nil {
			// A value YAML decoded always has a JSON form; one that does not
			// is written as its text rather than lost.
			buf.Reset()
			fmt.Fprintf(&buf, "%q", fmt.Sprint(ext[k]))
		}
		b.WriteString(renderYAMLKey(k))
		b.WriteString(": ")
		b.Write(bytes.TrimRight(buf.Bytes(), "\n"))
		b.WriteByte('\n')
	}
}

// renderYAMLKey writes an extension key bare when YAML reads it back as the
// same string, and double-quoted otherwise.
func renderYAMLKey(k string) string {
	for _, r := range k {
		if !(r == '-' || r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return strconv.Quote(k)
		}
	}
	return k
}

// jsonCompatible turns the maps a YAML decoder may return with non-string
// keys into maps JSON can encode.
func jsonCompatible(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = jsonCompatible(e)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[fmt.Sprint(k)] = jsonCompatible(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = jsonCompatible(e)
		}
		return out
	}
	return v
}
