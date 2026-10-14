package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// SchemaDialect is the JSON Schema dialect every generated document declares.
const SchemaDialect = "https://json-schema.org/draft/2020-12/schema"

// DescriptionTag is the struct tag a field's schema description is read
// from, beside its json tag. A doc comment is not visible to reflection, and
// a side table drifts from the field it describes; a tag sits on the field.
const DescriptionTag = "description"

// SchemaField is one key of a SchemaObject.
type SchemaField struct {
	Key   string
	Value any
}

// SchemaObject is a JSON object whose keys keep the order they were set in.
// A map would sort them, and in a schema a model reads, property order is
// visible: what a field is comes before what is inside it.
type SchemaObject []SchemaField

// MarshalJSON writes the keys in order.
func (o SchemaObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := compactJSON(f.Key)
		if err != nil {
			return nil, err
		}
		v, err := compactJSON(f.Value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Get returns the value of key, and whether it is set.
func (o SchemaObject) Get(key string) (any, bool) {
	for _, f := range o {
		if f.Key == key {
			return f.Value, true
		}
	}
	return nil, false
}

// Set replaces the value of key in place, or appends it.
func (o *SchemaObject) Set(key string, value any) {
	for i, f := range *o {
		if f.Key == key {
			(*o)[i].Value = value
			return
		}
	}
	*o = append(*o, SchemaField{Key: key, Value: value})
}

// without returns o minus the named keys.
func (o SchemaObject) without(keys map[string]bool) SchemaObject {
	out := make(SchemaObject, 0, len(o))
	for _, f := range o {
		if !keys[f.Key] {
			out = append(out, f)
		}
	}
	return out
}

// SchemaProvider is implemented by a type whose JSON form comes from its own
// MarshalJSON, so reflecting over its Go fields would describe the wrong
// document. It returns the schema of what it marshals to.
type SchemaProvider interface {
	JSONSchema() SchemaObject
}

var (
	schemaProviderType = reflect.TypeOf((*SchemaProvider)(nil)).Elem()
	marshalerType      = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	durationType       = reflect.TypeOf(time.Duration(0))
)

// SchemaFor describes a Go type as a JSON Schema node, reading it the way
// encoding/json does: a struct's properties in declaration order, named by
// their json tags, required unless omitempty; a slice as an array of its
// element; a pointer as its pointee; a map as an object of its values. Each
// property's description is read from its description tag.
func SchemaFor(t reflect.Type) SchemaObject {
	return schemaFor(t, nil)
}

func schemaFor(t reflect.Type, stack []reflect.Type) SchemaObject {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Implements(schemaProviderType) {
		return reflect.Zero(t).Interface().(SchemaProvider).JSONSchema()
	}
	switch t.Kind() {
	case reflect.Bool:
		return SchemaObject{{Key: "type", Value: "boolean"}}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return SchemaObject{{Key: "type", Value: "integer"}}
	case reflect.Float32, reflect.Float64:
		return SchemaObject{{Key: "type", Value: "number"}}
	case reflect.String:
		return SchemaObject{{Key: "type", Value: "string"}}
	case reflect.Slice, reflect.Array:
		return SchemaObject{{Key: "type", Value: "array"}, {Key: "items", Value: schemaFor(t.Elem(), stack)}}
	case reflect.Map:
		return SchemaObject{{Key: "type", Value: "object"}, {Key: "additionalProperties", Value: schemaFor(t.Elem(), stack)}}
	case reflect.Struct:
		if t.Implements(marshalerType) {
			return SchemaObject{}
		}
		for _, seen := range stack {
			if seen == t {
				return SchemaObject{{Key: "type", Value: "object"}}
			}
		}
		return structSchema(t, append(stack, t))
	}
	// An interface: any value.
	return SchemaObject{}
}

func structSchema(t reflect.Type, stack []reflect.Type) SchemaObject {
	props := SchemaObject{}
	required := []string{}
	for _, f := range reflect.VisibleFields(t) {
		if !f.IsExported() || f.Anonymous {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		node := withDescription(schemaFor(f.Type, stack), f.Tag.Get(DescriptionTag))
		props = append(props, SchemaField{Key: name, Value: node})
		if !strings.Contains(","+opts+",", ",omitempty,") {
			required = append(required, name)
		}
	}
	return SchemaObject{
		{Key: "type", Value: "object"},
		{Key: "properties", Value: props},
		{Key: "required", Value: required},
	}
}

// withDescription sets node's description right after its type. An empty
// desc leaves node as it is.
func withDescription(node SchemaObject, desc string) SchemaObject {
	if desc == "" {
		return node
	}
	out := make(SchemaObject, 0, len(node)+1)
	placed := false
	for _, f := range node {
		if f.Key == "description" {
			continue
		}
		out = append(out, f)
		if f.Key == "type" && !placed {
			out = append(out, SchemaField{Key: "description", Value: desc})
			placed = true
		}
	}
	if !placed {
		out = append(SchemaObject{{Key: "description", Value: desc}}, out...)
	}
	return out
}

// EnvelopeDocument is the standalone JSON Schema document of the envelope a
// command writes: Envelope with its result property replaced by the schema
// of result's type, and the named properties a command never sets left out.
// result is used for its type only, so a zero value does.
func EnvelopeDocument(result any, omit ...string) SchemaObject {
	drop := make(map[string]bool, len(omit))
	for _, k := range omit {
		drop[k] = true
	}
	env := SchemaFor(reflect.TypeOf(Envelope{}))
	props, _ := env.Get("properties")
	po := props.(SchemaObject).without(drop)
	if result != nil {
		var desc string
		if f, ok := reflect.TypeOf(Envelope{}).FieldByName("Result"); ok {
			desc = f.Tag.Get(DescriptionTag)
		}
		po.Set("result", withDescription(SchemaFor(reflect.TypeOf(result)), desc))
	}
	req, _ := env.Get("required")
	var required []string
	for _, k := range req.([]string) {
		if !drop[k] {
			required = append(required, k)
		}
	}
	return SchemaObject{
		{Key: "$schema", Value: SchemaDialect},
		{Key: "type", Value: "object"},
		{Key: "properties", Value: po},
		{Key: "required", Value: required},
	}
}

// FlagsDocument describes every flag on fs as a standalone JSON Schema
// document, in flag-name order, so a framework can use it as a tool's input
// schema. enums names the flags with a closed set of values. --schema and
// --version are left out: both return before any work is done. Nothing is
// required, since every flag has a default.
func FlagsDocument(fs *flag.FlagSet, enums map[string][]string) SchemaObject {
	props := SchemaObject{}
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name == "schema" || f.Name == "version" {
			return
		}
		typ, format, def := flagType(f)
		p := SchemaObject{{Key: "type", Value: typ}}
		if format != "" {
			p.Set("format", format)
		}
		if e, ok := enums[f.Name]; ok {
			p.Set("enum", e)
		}
		p.Set("default", def)
		p.Set("description", f.Usage)
		props = append(props, SchemaField{Key: f.Name, Value: p})
	})
	return SchemaObject{
		{Key: "$schema", Value: SchemaDialect},
		{Key: "type", Value: "object"},
		{Key: "properties", Value: props},
	}
}

// flagType infers a flag's JSON type, format and typed default from its
// Value. A Value that is not a flag.Getter is a string.
func flagType(f *flag.Flag) (typ, format string, def any) {
	g, ok := f.Value.(flag.Getter)
	if !ok {
		return "string", "", f.DefValue
	}
	switch v := g.Get().(type) {
	case bool:
		b, _ := strconv.ParseBool(f.DefValue)
		return "boolean", "", b
	case int, int64, uint, uint64:
		n, _ := strconv.ParseInt(f.DefValue, 10, 64)
		return "integer", "", n
	case float64:
		x, _ := strconv.ParseFloat(f.DefValue, 64)
		return "number", "", x
	default:
		if reflect.TypeOf(v) == durationType {
			return "string", "duration", f.DefValue
		}
	}
	return "string", "", f.DefValue
}
