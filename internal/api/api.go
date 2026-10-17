// Package api is hive's method registry. Every method has a name, a
// description, and Go input and output types; the registry derives JSON
// Schema from the types, decodes arguments strictly, and describes itself, so
// the CLI and scripts can bind to whatever the server offers.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// Error is a failure with an HTTP status and a stable code.
type Error struct {
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (status=%d code=%s)", e.Message, e.Status, e.Code)
}

func newErr(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

func BadRequest(format string, args ...any) *Error {
	return newErr(http.StatusBadRequest, "bad_request", format, args...)
}
func NotFound(format string, args ...any) *Error {
	return newErr(http.StatusNotFound, "not_found", format, args...)
}
func Ambiguous(details any, format string, args ...any) *Error {
	e := newErr(http.StatusConflict, "ambiguous", format, args...)
	e.Details = details
	return e
}
func Conflict(format string, args ...any) *Error {
	return newErr(http.StatusConflict, "conflict", format, args...)
}
func Internal(format string, args ...any) *Error {
	return newErr(http.StatusInternalServerError, "internal", format, args...)
}

// Method is a registered method and its documentation.
type Method struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"input_schema"`
	OutputSchema map[string]any `json:"output_schema"`

	inType  reflect.Type
	handler func(ctx context.Context, raw json.RawMessage) (any, error)
}

// Registry holds methods by name.
type Registry struct {
	methods map[string]*Method
}

// New makes an empty registry.
func New() *Registry { return &Registry{methods: map[string]*Method{}} }

// Register adds a method. I and O must be structs.
func Register[I, O any](r *Registry, name, description string, fn func(ctx context.Context, in I) (O, error)) {
	it, ot := reflect.TypeFor[I](), reflect.TypeFor[O]()
	m := &Method{
		Name: name, Description: description,
		InputSchema: Schema(it), OutputSchema: Schema(ot), inType: it,
	}
	m.handler = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in I
		if err := decode(it, raw, &in); err != nil {
			return nil, err
		}
		return fn(ctx, in)
	}
	r.methods[name] = m
}

// List returns every method, sorted by name.
func (r *Registry) List() []*Method {
	out := make([]*Method, 0, len(r.methods))
	for _, m := range r.methods {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Describe returns one method.
func (r *Registry) Describe(name string) (*Method, bool) {
	m, ok := r.methods[name]
	return m, ok
}

// Call runs a method with JSON arguments.
func (r *Registry) Call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	m, ok := r.methods[name]
	if !ok {
		return nil, newErr(http.StatusNotFound, "method_not_found", "no method named %q", name)
	}
	return m.handler(ctx, raw)
}

func decode(t reflect.Type, raw json.RawMessage, into any) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return BadRequest("input: arguments must be a JSON object: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return BadRequest("input: %v", err)
	}
	return validate(t, fields, reflect.ValueOf(into).Elem())
}

// validate checks required fields and enums, which encoding/json doesn't.
func validate(t reflect.Type, fields map[string]json.RawMessage, v reflect.Value) error {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := jsonName(f)
		if name == "" {
			continue
		}
		if f.Tag.Get("required") == "true" {
			if _, ok := fields[name]; !ok {
				return BadRequest("input: %s is required", name)
			}
		}
		if enum := f.Tag.Get("enum"); enum != "" && f.Type.Kind() == reflect.String {
			s := v.Field(i).String()
			if s != "" && !slices.Contains(strings.Split(enum, ","), s) {
				return BadRequest("input: %s must be one of %s, not %q", name, enum, s)
			}
		}
	}
	return nil
}

func jsonName(f reflect.StructField) string {
	if !f.IsExported() {
		return ""
	}
	tag := f.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		name = f.Name
	}
	return name
}

// Schema derives a JSON Schema for a Go type.
func Schema(t reflect.Type) map[string]any {
	return schema(t, map[reflect.Type]bool{})
}

func schema(t reflect.Type, seen map[reflect.Type]bool) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer", "minimum": 0}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "contentEncoding": "base64"}
		}
		return map[string]any{"type": "array", "items": schema(t.Elem(), seen)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schema(t.Elem(), seen)}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Struct:
		if t.String() == "time.Time" {
			return map[string]any{"type": "string", "format": "date-time"}
		}
		if t.String() == "json.RawMessage" {
			return map[string]any{}
		}
		if seen[t] {
			return map[string]any{"type": "object"}
		}
		seen[t] = true
		defer delete(seen, t)
		props := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := jsonName(f)
			if name == "" {
				continue
			}
			p := schema(f.Type, seen)
			if d := f.Tag.Get("desc"); d != "" {
				p["description"] = d
			}
			if e := f.Tag.Get("enum"); e != "" {
				p["enum"] = strings.Split(e, ",")
			}
			props[name] = p
			if f.Tag.Get("required") == "true" {
				required = append(required, name)
			}
		}
		s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	return map[string]any{}
}
