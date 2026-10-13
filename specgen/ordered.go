package specgen

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"io"
)

// valueKind is the JSON type of an orderedValue.
type valueKind int

const (
	kindNull valueKind = iota
	kindBool
	kindNumber
	kindString
	kindArray
	kindObject
)

// orderedValue is a decoded JSON value that keeps the order an object's
// members were written in, which encoding/json's map decoding loses. The
// schema converter needs it: property order is model-visible and part of the
// provider's cache prefix.
type orderedValue struct {
	Kind   valueKind
	Scalar json.RawMessage // the literal of a null, bool, number or string
	Array  []orderedValue
	Object orderedObject
}

// member is one key and value of an orderedObject.
type member struct {
	Key   string
	Value orderedValue
}

// orderedObject is an object's members in authored order.
type orderedObject []member

// Get returns the value of key, and whether it is present.
func (o orderedObject) Get(key string) (orderedValue, bool) {
	for _, m := range o {
		if m.Key == key {
			return m.Value, true
		}
	}
	return orderedValue{}, false
}

// decodeOrdered decodes one JSON document, refusing trailing data and a
// duplicate key.
func decodeOrdered(data []byte) (orderedValue, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(data))
	v, err := readValue(dec)
	if err != nil {
		return orderedValue{}, err
	}
	if _, err := dec.ReadToken(); err != io.EOF {
		return orderedValue{}, errors.New("trailing data after the document")
	}
	return v, nil
}

func readValue(dec *jsontext.Decoder) (orderedValue, error) {
	switch dec.PeekKind() {
	case '{':
		if _, err := dec.ReadToken(); err != nil {
			return orderedValue{}, err
		}
		v := orderedValue{Kind: kindObject, Object: orderedObject{}}
		for dec.PeekKind() != '}' {
			tok, err := dec.ReadToken()
			if err != nil {
				return orderedValue{}, err
			}
			key := tok.String() // a token is void after the decoder's next call
			val, err := readValue(dec)
			if err != nil {
				return orderedValue{}, err
			}
			v.Object = append(v.Object, member{Key: key, Value: val})
		}
		_, err := dec.ReadToken()
		return v, err
	case '[':
		if _, err := dec.ReadToken(); err != nil {
			return orderedValue{}, err
		}
		v := orderedValue{Kind: kindArray, Array: []orderedValue{}}
		for dec.PeekKind() != ']' {
			val, err := readValue(dec)
			if err != nil {
				return orderedValue{}, err
			}
			v.Array = append(v.Array, val)
		}
		_, err := dec.ReadToken()
		return v, err
	}
	raw, err := dec.ReadValue()
	if err != nil {
		return orderedValue{}, err
	}
	v := orderedValue{Scalar: json.RawMessage(bytes.Clone(raw))}
	switch raw.Kind() {
	case 'n':
		v.Kind = kindNull
	case 't', 'f':
		v.Kind = kindBool
	case '0':
		v.Kind = kindNumber
	case '"':
		v.Kind = kindString
	}
	return v, nil
}

// MarshalJSON writes the value back with its members in authored order.
func (v orderedValue) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	switch v.Kind {
	case kindObject:
		b.WriteByte('{')
		for i, m := range v.Object {
			if i > 0 {
				b.WriteByte(',')
			}
			k, err := json.Marshal(m.Key)
			if err != nil {
				return nil, err
			}
			b.Write(k)
			b.WriteByte(':')
			val, err := m.Value.MarshalJSON()
			if err != nil {
				return nil, err
			}
			b.Write(val)
		}
		b.WriteByte('}')
	case kindArray:
		b.WriteByte('[')
		for i, e := range v.Array {
			if i > 0 {
				b.WriteByte(',')
			}
			val, err := e.MarshalJSON()
			if err != nil {
				return nil, err
			}
			b.Write(val)
		}
		b.WriteByte(']')
	default:
		if len(v.Scalar) == 0 {
			return []byte("null"), nil
		}
		b.Write(v.Scalar)
	}
	return b.Bytes(), nil
}
