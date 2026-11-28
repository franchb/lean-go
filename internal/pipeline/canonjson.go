package pipeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Canonical JSON for the artifacts the pipeline tracks. Decoding is strict:
// one document of valid UTF-8, with duplicate keys and non-finite constants
// as errors. Encoding has three fixed layouts, so equal values always give
// equal bytes: compact with sorted keys (IR), indented by two spaces with
// sorted keys (evidence), and compact in insertion order (diagnostics).
// Characters outside ASCII are written as they are; the only numbers are
// integers.

// Object is a JSON object that keeps its keys in document order.
type Object struct {
	Keys   []string
	Values map[string]any
}

// NewObject returns an empty ordered object.
func NewObject() *Object { return &Object{Values: map[string]any{}} }

// Get returns a member and whether it exists.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.Values[key]
	return v, ok
}

// Set replaces an existing member in place or appends a new one.
func (o *Object) Set(key string, value any) {
	if _, ok := o.Values[key]; !ok {
		o.Keys = append(o.Keys, key)
	}
	o.Values[key] = value
}

// Delete removes a member.
func (o *Object) Delete(key string) {
	if _, ok := o.Values[key]; !ok {
		return
	}
	delete(o.Values, key)
	for i, k := range o.Keys {
		if k == key {
			o.Keys = append(o.Keys[:i:i], o.Keys[i+1:]...)
			break
		}
	}
}

// HasExactly reports whether the object's key set equals keys.
func (o *Object) HasExactly(keys ...string) bool {
	if len(o.Keys) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := o.Values[k]; !ok {
			return false
		}
	}
	return true
}

// ParseJSON decodes one strict JSON document. Objects become *Object, arrays
// []any, strings string, numbers json.Number, and true/false/null bool/nil.
func ParseJSON(data []byte) (any, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		return nil, errors.New("invalid JSON: input is not valid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	value, err := readValue(d, 0)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("invalid JSON: extra data after the document")
	}
	return value, nil
}

func readValue(d *json.Decoder, depth int) (any, error) {
	if depth > 10000 {
		return nil, errors.New("nesting too deep")
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewObject()
			for d.More() {
				keyToken, err := d.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("object key must be a string")
				}
				if _, exists := obj.Values[key]; exists {
					return nil, fmt.Errorf("duplicate JSON key: %s", key)
				}
				value, err := readValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				obj.Set(key, value)
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			list := []any{}
			for d.More() {
				value, err := readValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				list = append(list, value)
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return list, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	default:
		return t, nil
	}
}

// Encoding styles.
type jsonStyle struct {
	sorted bool
	indent bool
}

var (
	compactSorted  = jsonStyle{sorted: true}
	indentedSorted = jsonStyle{sorted: true, indent: true}
	compactOrdered = jsonStyle{}
)

// EncodeCompactSorted writes v on one line with sorted keys and no space after "," or ":".
func EncodeCompactSorted(v any) ([]byte, error) { return encode(v, compactSorted) }

// EncodeIndentedSorted writes v with sorted keys, one member or element per
// line indented by two spaces, and ": " after a key. Empty containers stay on
// one line.
func EncodeIndentedSorted(v any) ([]byte, error) { return encode(v, indentedSorted) }

// EncodeCompactOrdered writes v on one line with keys in insertion order and
// no space after "," or ":".
func EncodeCompactOrdered(v any) ([]byte, error) { return encode(v, compactOrdered) }

func encode(v any, style jsonStyle) ([]byte, error) {
	var b bytes.Buffer
	if err := writeValue(&b, v, style, 0); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func newline(b *bytes.Buffer, style jsonStyle, level int) {
	if style.indent {
		b.WriteByte('\n')
		b.WriteString(strings.Repeat("  ", level))
	}
}

func writeValue(b *bytes.Buffer, v any, style jsonStyle, level int) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		writeString(b, t)
	case json.Number:
		text, err := canonicalInteger(string(t))
		if err != nil {
			return err
		}
		b.WriteString(text)
	case int:
		b.WriteString(strconv.Itoa(t))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case []string:
		list := make([]any, len(t))
		for i, s := range t {
			list[i] = s
		}
		return writeValue(b, list, style, level)
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return nil
		}
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(b, style, level+1)
			if err := writeValue(b, item, style, level+1); err != nil {
				return err
			}
		}
		newline(b, style, level)
		b.WriteByte(']')
	case map[string]string:
		obj := NewObject()
		for k, s := range t {
			obj.Set(k, s)
		}
		sort.Strings(obj.Keys)
		return writeValue(b, obj, style, level)
	case *Object:
		if len(t.Keys) == 0 {
			b.WriteString("{}")
			return nil
		}
		keys := t.Keys
		if style.sorted {
			keys = append([]string(nil), keys...)
			sort.Strings(keys)
		}
		b.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(b, style, level+1)
			writeString(b, key)
			b.WriteByte(':')
			if style.indent {
				b.WriteByte(' ')
			}
			if err := writeValue(b, t.Values[key], style, level+1); err != nil {
				return err
			}
		}
		newline(b, style, level)
		b.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value %T", v)
	}
	return nil
}

// canonicalInteger is the encoding of an integer literal: the literal itself,
// except that -0 is 0. The producers never emit fractions or exponents, so
// those fail.
func canonicalInteger(text string) (string, error) {
	if strings.ContainsAny(text, ".eE") {
		return "", fmt.Errorf("unsupported non-integer JSON number %s", text)
	}
	if text == "-0" {
		return "0", nil
	}
	return text, nil
}

func writeString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// quoted renders a string for an error message: in single quotes, or in
// double quotes when it contains a single quote and no double quote, with
// backslash escapes for the quote, the backslash, control characters and
// characters that do not print.
func quoted(s string) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == rune(quote):
			b.WriteByte('\\')
			b.WriteByte(quote)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == ' ' || (r != utf8.RuneError && isPrintable(r)):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}
