package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"
)

// TemplatedBool accepts a literal JSON boolean or a Go template string that
// renders to "true" or "false". Playbook actions are decoded before runtime
// templates are evaluated, so a bool field cannot hold a template expression.
// Keeping the original JSON representation allows both forms while Resolve
// provides a strict boolean to action implementations.
type TemplatedBool json.RawMessage

// JSONSchema documents the two representations accepted before runtime
// templating: a literal boolean or a template string. Kubernetes CRDs require
// the fields to remain schemaless because structural schemas do not support a
// boolean-or-string primitive union, but generated JSON schemas can expose it.
func (TemplatedBool) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Description: "A literal true or false, or a template string that renders to true or false.",
		OneOf: []*jsonschema.Schema{
			{Type: "boolean"},
			{Type: "string"},
		},
	}
}

// UnmarshalJSON accepts only true, false, null, or a string containing a
// template that will be validated after rendering.
func (value *TemplatedBool) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*value = nil
		return nil
	}

	var literal bool
	if err := json.Unmarshal(data, &literal); err == nil {
		*value = append((*value)[:0], data...)
		return nil
	}

	var template string
	if err := json.Unmarshal(data, &template); err == nil {
		*value = append((*value)[:0], data...)
		return nil
	}

	return fmt.Errorf("expected true, false, or a template string")
}

// MarshalJSON preserves the literal boolean or template string supplied by the
// playbook.
func (value TemplatedBool) MarshalJSON() ([]byte, error) {
	if len(value) == 0 {
		return []byte("null"), nil
	}
	if !json.Valid(value) {
		return nil, fmt.Errorf("invalid templated boolean JSON")
	}
	return append([]byte(nil), value...), nil
}

// Resolve converts a literal or rendered template value to a boolean. An empty
// value uses defaultValue.
func (value TemplatedBool) Resolve(defaultValue bool) (bool, error) {
	if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return defaultValue, nil
	}

	var literal bool
	if err := json.Unmarshal(value, &literal); err == nil {
		return literal, nil
	}

	var rendered string
	if err := json.Unmarshal(value, &rendered); err != nil {
		return false, fmt.Errorf("expected true, false, or a template string")
	}

	switch strings.ToLower(strings.TrimSpace(rendered)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("template rendered %q; expected true or false", rendered)
	}
}
