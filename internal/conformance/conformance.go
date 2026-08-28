// Package conformance validates that decoded Go models stay in sync with the
// raw JSON payloads returned by the FPL API.
//
// The library's contract with its users is schema-shaped, not value-shaped:
// when the API renames, adds, or removes a field, json.Unmarshal silently
// zero-values the affected model fields and the drift goes unnoticed. Check
// closes that gap by comparing a raw payload against the decoded model:
//
//   - unmapped: every key in the payload must map to a model field (or be on
//     the spec's Allowlist, marking it as a deliberate non-mapping decision).
//     This catches API additions and mistyped json tags.
//   - orphaned: every model field must have a corresponding payload key.
//     This catches API removals and renames, which json.Unmarshal would
//     otherwise silently turn into zero values.
//   - fidelity: scalar payload values must equal the decoded field values.
//     A model decoded from this very payload always satisfies this; it fires
//     when the model comes from a different payload than the one checked —
//     a stale fixture, live data that changed between two fetches, or a
//     harness wiring mistake.
package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"
)

// Spec describes how a raw JSON payload maps onto a decoded model.
type Spec struct {
	// Model is the decoded value to validate, e.g. &resp or resp.Results[0].
	Model any

	// Allowlist lists API keys that appear in payloads but are deliberately
	// not mapped onto the model. Adding a key here is an explicit, reviewable
	// decision to ignore that field.
	Allowlist []string
}

// Check asserts that raw and spec.Model agree on schema and scalar fidelity.
// The model may be a struct, a pointer to one, or a slice (of structs or
// pointers to structs); for slices the first payload element is compared.
func Check(t testing.TB, raw []byte, spec Spec) {
	t.Helper()

	if spec.Model == nil {
		t.Fatal("conformance: Spec.Model must not be nil")
	}

	payload, err := decodeValue(raw)
	if err != nil {
		t.Fatalf("conformance: payload is not valid JSON: %v", err)
	}

	allow := map[string]bool{}
	for _, k := range spec.Allowlist {
		allow[k] = true
	}

	check(t, payload, reflect.ValueOf(spec.Model), allow)
}

// Extract navigates path within raw (string segments index objects, int
// segments index arrays) and returns the subtree as compact JSON. It is used
// to pull nested elements out of a captured payload so they can be checked
// against their model type.
func Extract(t testing.TB, raw []byte, path ...any) []byte {
	t.Helper()

	node, err := decodeValue(raw)
	if err != nil {
		t.Fatalf("conformance: payload is not valid JSON: %v", err)
	}

	for _, seg := range path {
		switch s := seg.(type) {
		case string:
			obj, ok := node.(map[string]any)
			if !ok {
				t.Fatalf("conformance: path segment %q: payload node is not an object", s)
			}
			node = obj[s]
		case int:
			arr, ok := node.([]any)
			if !ok {
				t.Fatalf("conformance: path segment %d: payload node is not an array", s)
			}
			if s < 0 || s >= len(arr) {
				t.Fatalf("conformance: path segment %d: index out of range (len %d)", s, len(arr))
			}
			node = arr[s]
		default:
			t.Fatalf("conformance: path segments must be string or int, got %T", seg)
		}
		if node == nil {
			t.Fatalf("conformance: path %v leads to a null node", path)
		}
	}

	out, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("conformance: failed to re-encode extracted node: %v", err)
	}
	return out
}

// Report returns the sorted list of payload keys that are neither mapped on
// the model nor allowlisted. It powers the "fields available but unmapped"
// inventory; tests can surface it as a backlog indicator.
func Report(t testing.TB, raw []byte, spec Spec) []string {
	t.Helper()

	if spec.Model == nil {
		t.Fatal("conformance: Spec.Model must not be nil")
	}

	payload, err := decodeValue(raw)
	if err != nil {
		t.Fatalf("conformance: payload is not valid JSON: %v", err)
	}

	obj, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("conformance: payload is not a JSON object")
	}

	mt := structType(reflect.ValueOf(spec.Model))
	if mt == nil || mt.Kind() != reflect.Struct {
		t.Fatalf("conformance: Spec.Model must be a populated struct or slice of structs")
	}
	tags := jsonTags(mt)
	allow := map[string]bool{}
	for _, k := range spec.Allowlist {
		allow[k] = true
	}

	var unmapped []string
	for key := range obj {
		if _, mapped := tags[key]; !mapped && !allow[key] {
			unmapped = append(unmapped, key)
		}
	}
	sort.Strings(unmapped)
	return unmapped
}

func check(t testing.TB, payload any, model reflect.Value, allow map[string]bool) {
	t.Helper()

	model = indirect(model)
	if !model.IsValid() {
		t.Fatal("conformance: Spec.Model is a nil pointer; decode the payload into it first")
	}

	switch model.Kind() {
	case reflect.Slice:
		arr, ok := payload.([]any)
		if !ok {
			t.Fatalf("conformance: payload is not a JSON array, but the model is a slice (%s)", model.Type())
		}
		if model.Len() != len(arr) {
			t.Errorf("conformance: model has %d elements, payload has %d", model.Len(), len(arr))
		}
		// An empty model (or payload) leaves nothing to compare element-wise;
		// the length mismatch above already reports the discrepancy.
		if len(arr) == 0 || model.Len() == 0 {
			return
		}
		check(t, arr[0], model.Index(0), allow)

	case reflect.Struct:
		obj, ok := payload.(map[string]any)
		if !ok {
			t.Fatalf("conformance: payload is not a JSON object, but the model is a struct (%s)", model.Type())
		}
		checkStruct(t, obj, model, allow)

	default:
		t.Fatalf("conformance: unsupported model kind %s (want struct or slice of structs)", model.Kind())
	}
}

func checkStruct(t testing.TB, obj map[string]any, model reflect.Value, allow map[string]bool) {
	t.Helper()

	tags := jsonTags(model.Type())

	var unmapped, orphaned []string
	for key := range obj {
		if _, mapped := tags[key]; !mapped && !allow[key] {
			unmapped = append(unmapped, key)
		}
	}
	for key, field := range tags {
		if _, present := obj[key]; !present {
			orphaned = append(orphaned, fmt.Sprintf("%s.%s", model.Type(), field))
			continue
		}
		if err := compareScalars(field, obj[key], model); err != nil {
			t.Errorf("conformance: field %s.%s does not match payload value %v: %v",
				model.Type(), field, obj[key], err)
		}
	}

	sort.Strings(unmapped)
	for _, key := range unmapped {
		t.Errorf("conformance: API key %q is not mapped on %s — add it to the model or to the spec Allowlist",
			key, model.Type())
	}
	sort.Strings(orphaned)
	for _, field := range orphaned {
		t.Errorf("conformance: model field %s has no corresponding API key — the API may have removed or renamed it", field)
	}
}

// compareScalars verifies that the decoded struct field equals the raw JSON
// value for its key. Objects, arrays, and JSON nulls are skipped: null decodes
// to a zero value by design, and nested structures are checked recursively
// elsewhere.
func compareScalars(fieldName string, raw any, model reflect.Value) error {
	if raw == nil {
		return nil
	}

	field, ok := fieldByName(model, fieldName)
	if !ok {
		return fmt.Errorf("field not found")
	}

	for field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return fmt.Errorf("payload has a value but the field is a nil pointer")
		}
		field = field.Elem()
	}

	// Timestamps arrive as RFC 3339 strings but decode into time.Time
	// structs; compare them as instants rather than by kind.
	if field.Type() == timeType {
		s, isStr := raw.(string)
		if !isStr {
			return fmt.Errorf("payload is a %T but the field is a time.Time", raw)
		}
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return fmt.Errorf("payload timestamp %q is not RFC 3339", s)
		}
		if !field.Interface().(time.Time).Equal(parsed) {
			return fmt.Errorf("field has %s", field.Interface().(time.Time).Format(time.RFC3339))
		}
		return nil
	}

	switch v := raw.(type) {
	case string:
		if field.Kind() != reflect.String {
			return fmt.Errorf("payload is a string but the field is %s", field.Kind())
		}
		if field.String() != v {
			return fmt.Errorf("field has %q", field.String())
		}
	case bool:
		if field.Kind() != reflect.Bool {
			return fmt.Errorf("payload is a bool but the field is %s", field.Kind())
		}
		if field.Bool() != v {
			return fmt.Errorf("field has %v", field.Bool())
		}
	case json.Number:
		if err := compareNumber(v, field); err != nil {
			return err
		}
	case map[string]any, []any:
		return nil // nested values are validated by their own Check call
	default:
		return fmt.Errorf("unsupported payload type %T", raw)
	}
	return nil
}

func compareNumber(v json.Number, field reflect.Value) error {
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := v.Int64()
		if err != nil {
			return fmt.Errorf("payload number %s does not fit the int field", v.String())
		}
		if field.Int() != i {
			return fmt.Errorf("field has %d", field.Int())
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := v.Int64() // JSON numbers are never negative in FPL payloads
		if err != nil {
			return fmt.Errorf("payload number %s does not fit the uint field", v.String())
		}
		if field.Uint() != uint64(u) {
			return fmt.Errorf("field has %d", field.Uint())
		}
	case reflect.Float32, reflect.Float64:
		f, err := v.Float64()
		if err != nil {
			return fmt.Errorf("payload number %s is not a valid float", v.String())
		}
		if field.Float() != f {
			return fmt.Errorf("field has %f", field.Float())
		}
	default:
		return fmt.Errorf("payload is a number but the field is %s", field.Kind())
	}
	return nil
}

// jsonTags maps JSON key -> Go field name for a struct type, flattening
// embedded structs. Fields tagged "-" are skipped.
func jsonTags(t reflect.Type) map[string]string {
	tags := map[string]string{}
	collectTags(t, tags)
	return tags
}

func collectTags(t reflect.Type, tags map[string]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := tag
		if idx := indexByte(tag, ','); idx >= 0 {
			name = tag[:idx]
		}
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			collectTags(f.Type, tags)
			continue
		}
		if name == "" {
			name = f.Name
		}
		tags[name] = f.Name
	}
}

func fieldByName(model reflect.Value, name string) (reflect.Value, bool) {
	f := model.FieldByName(name)
	return f, f.IsValid()
}

var timeType = reflect.TypeFor[time.Time]()

func indirect(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func structType(v reflect.Value) reflect.Type {
	t := indirect(v)
	if !t.IsValid() {
		return nil
	}
	if t.Kind() == reflect.Slice {
		// Dereference pointer element types so []*T behaves like []T.
		elem := t.Type().Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		return elem
	}
	return t.Type()
}

func decodeValue(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var payload any
	if err := dec.Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
