// Package wireformat turns a declarative JSON description of a fixed-size
// binary header into ordinary first-order Lean source, machine-checkable
// theorems about that source, and a Go []byte adapter over the package the
// lean-go extractor produces from it.
//
// The package only generates text. Running the extractor and the proof
// checker is isolated in pipeline.go.
package wireformat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Spec is the JSON document a developer writes. See docs/wire-formats.md.
type Spec struct {
	Name        string      `json:"name"`
	Module      string      `json:"module,omitempty"`
	Description string      `json:"description,omitempty"`
	Fields      []FieldSpec `json:"fields"`
}

// Constraints are shared by integer fields and bit-field members.
type Constraints struct {
	Const    *Number  `json:"const,omitempty"`
	Reserved bool     `json:"reserved,omitempty"`
	Enum     []Number `json:"enum,omitempty"`
	Min      *Number  `json:"min,omitempty"`
	Max      *Number  `json:"max,omitempty"`
}

// FieldSpec is either a byte-aligned unsigned integer (Type set) or a
// big-endian word split into bit-fields (Bitfield and Bits set).
type FieldSpec struct {
	Name          string       `json:"name,omitempty"`
	Type          string       `json:"type,omitempty"`
	Endian        string       `json:"endian,omitempty"`
	PayloadLength *PayloadSpec `json:"payload_length,omitempty"`
	Bitfield      string       `json:"bitfield,omitempty"`
	Bits          []BitSpec    `json:"bits,omitempty"`
	Constraints
}

// BitSpec is one member of a bit-field word, listed from the most significant
// bit unless every member gives an explicit LSB position.
type BitSpec struct {
	Name  string `json:"name"`
	Width int    `json:"width"`
	LSB   *int   `json:"lsb,omitempty"`
	Constraints
}

// PayloadSpec marks an integer field as the length of the payload that
// follows the header.
type PayloadSpec struct {
	IncludesHeader bool `json:"includes_header,omitempty"`
}

// Number is an unsigned 64-bit JSON value: a non-negative integer literal or
// a string holding a decimal or 0x-prefixed hexadecimal integer.
type Number uint64

func (n *Number) UnmarshalJSON(data []byte) error {
	text := string(data)
	if strings.HasPrefix(text, `"`) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		base, digits := 10, s
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			base, digits = 16, s[2:]
		}
		if digits == "" || strings.ContainsAny(digits, "+-_ ") {
			return fmt.Errorf("invalid number %q", s)
		}
		v, err := strconv.ParseUint(digits, base, 64)
		if err != nil {
			return fmt.Errorf("invalid number %q: out of range or malformed", s)
		}
		*n = Number(v)
		return nil
	}
	if text == "" || strings.ContainsAny(text, ".eE+-") {
		return fmt.Errorf("invalid number %s: use a non-negative integer", text)
	}
	v, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid number %s: out of range for 64 bits", text)
	}
	*n = Number(v)
	return nil
}

// DecodeSpec strictly decodes a JSON spec: duplicate keys, unknown keys and
// trailing data are rejected.
func DecodeSpec(data []byte) (*Spec, error) {
	if err := checkDuplicateKeys(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var spec Spec
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("spec: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("spec: unexpected data after the JSON object")
	}
	return &spec, nil
}

func checkDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var walk func(path string) error
	walk = func(path string) error {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("spec: %w", err)
		}
		switch tok {
		case json.Delim('{'):
			seen := map[string]bool{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return fmt.Errorf("spec: %w", err)
				}
				key := keyTok.(string)
				if seen[key] {
					return fmt.Errorf("spec: duplicate key %q in %s", key, path)
				}
				seen[key] = true
				if err := walk(path + "." + key); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case json.Delim('['):
			for i := 0; dec.More(); i++ {
				if err := walk(fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
		return nil
	}
	return walk("$")
}
