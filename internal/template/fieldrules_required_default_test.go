package template

import (
	"reflect"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-07.task-1: the
// required+default rule and the split between the schema-only check and the
// site that materialises a default (requirement field-rules).
//
//   - CheckValues is SCHEMA-ONLY: an omitted required field that declares a
//     Default yields no error, an omitted required field with no Default still
//     errors, and a supplied value is still type-checked. CheckValues never
//     writes the default into the data it was given.
//   - ResolveSectionCall's returned call.Values is where a section-helper
//     call's defaults ARE materialised (applyFieldDefaults): an omitted
//     required field's Default is present in call.Values, and a
//     caller-supplied value overrides the default.
//
// The schema-only half complements checkvalues_test.go, whose required cases
// carry no Default; the materialisation half complements
// sectionhelper_resolve_test.go's TestResolveSectionCallDefaultsFirst by
// covering several default value kinds, including a falsy default and a falsy
// caller-supplied override.

// TestCheckValuesRequiredDefaultIsSchemaOnly proves CheckValues treats an
// omitted required field that declares a Default as satisfied without
// materialising anything: it reports no error and leaves the caller's data
// untouched. An undefaulted omitted required field is still reported, and a
// supplied value is still type-checked.
func TestCheckValuesRequiredDefaultIsSchemaOnly(t *testing.T) {
	t.Run("an omitted required field with a default yields no error and is not materialised", func(t *testing.T) {
		tmpl := newSlide("footer", Field{Name: "title", Type: FieldText, Required: true, Default: "Untitled"})
		data := map[string]any{}

		got := CheckValues(data, tmpl, nil)
		if len(got.Errors) != 0 {
			t.Fatalf("CheckValues() errors = %s, want none: the required field carries a default", renderErrors(got.Errors))
		}
		if _, present := data["title"]; present {
			t.Errorf("CheckValues added %q to the caller's data: %v; it is schema-only and must not materialise the default", "title", data)
		}
	})

	t.Run("a falsy default still satisfies the required rule", func(t *testing.T) {
		// Default is a non-nil interface holding 0, so it counts as a declared
		// default even though it is falsy: the schema check keys on nil, not
		// on truthiness.
		tmpl := newSlide("counter", Field{Name: "count", Type: FieldNumber, Required: true, Default: 0})
		got := CheckValues(map[string]any{}, tmpl, nil)
		if len(got.Errors) != 0 {
			t.Fatalf("CheckValues() errors = %s, want none: a 0 default is still a default", renderErrors(got.Errors))
		}
	})

	t.Run("an omitted required field with no default still errors", func(t *testing.T) {
		tmpl := newSlide("footer", Field{Name: "title", Type: FieldText, Required: true})
		got := CheckValues(map[string]any{}, tmpl, nil)
		if len(got.Errors) != 1 {
			t.Fatalf("CheckValues() errors = %s, want exactly the required error", renderErrors(got.Errors))
		}
		e := got.Errors[0]
		if e.Rule != "required" || e.PathString() != "title" {
			t.Errorf("error = rule %q path %q, want rule %q path %q", e.Rule, e.PathString(), "required", "title")
		}
	})

	t.Run("a supplied value is still type-checked despite a default", func(t *testing.T) {
		tmpl := newSlide("footer", Field{Name: "title", Type: FieldText, Required: true, Default: "Untitled"})
		got := CheckValues(map[string]any{"title": 42}, tmpl, nil)
		if len(got.Errors) != 1 {
			t.Fatalf("CheckValues() errors = %s, want the type error", renderErrors(got.Errors))
		}
		e := got.Errors[0]
		if e.Rule != "type" || e.PathString() != "title" {
			t.Errorf("error = rule %q path %q, want rule %q path %q", e.Rule, e.PathString(), "type", "title")
		}
	})
}

// TestResolveSectionCallMaterialisesRequiredDefault proves the required
// materialisation site: ResolveSectionCall returns call.Values with the
// target's declared defaults filled in (applyFieldDefaults), including a
// required field's default and a falsy default, and a caller-supplied value
// overrides the default — even a falsy supplied value.
func TestResolveSectionCallMaterialisesRequiredDefault(t *testing.T) {
	target := &Template{
		Name:  "footer",
		Usage: UsageSection,
		Fields: []Field{
			{Name: "title", Type: FieldText, Required: true, Default: "Untitled"},
			{Name: "count", Type: FieldNumber, Default: 5},
			{Name: "flag", Type: FieldBoolean, Default: false},
		},
	}
	resolve := resolveSectionCallResolver(target)

	t.Run("an omitted required field's default is materialised into call.Values", func(t *testing.T) {
		call, err := ResolveSectionCall(resolve, "footer", nil, "", nil)
		if err != nil {
			t.Fatalf("ResolveSectionCall() error = %v, want nil: the required field carries a default", err)
		}
		if got := call.Values["title"]; got != "Untitled" {
			t.Errorf("call.Values[title] = %v, want the materialised default %q", got, "Untitled")
		}
		if got := call.Values["count"]; got != 5 {
			t.Errorf("call.Values[count] = %v, want the materialised default 5", got)
		}
		if got, present := call.Values["flag"]; !present {
			t.Errorf("call.Values[flag] is absent, want the falsy default false materialised: %v", call.Values)
		} else if got != false {
			t.Errorf("call.Values[flag] = %v, want the materialised falsy default false", got)
		}
		if len(call.Values) != 3 {
			t.Errorf("call.Values = %v, want exactly the three declared defaults", call.Values)
		}
	})

	t.Run("a caller-supplied value overrides the default", func(t *testing.T) {
		call, err := ResolveSectionCall(resolve, "footer",
			map[string]any{"title": "Given", "count": 0}, "", nil)
		if err != nil {
			t.Fatalf("ResolveSectionCall() error = %v, want nil", err)
		}
		if got := call.Values["title"]; got != "Given" {
			t.Errorf("call.Values[title] = %v, want the caller-supplied %q", got, "Given")
		}
		// A falsy supplied value still wins: the default fills only an absent
		// field, never an explicitly supplied one.
		if got := call.Values["count"]; got != 0 {
			t.Errorf("call.Values[count] = %v, want the caller-supplied 0", got)
		}
		want := map[string]any{"title": "Given", "count": 0, "flag": false}
		if !reflect.DeepEqual(call.Values, want) {
			t.Errorf("call.Values = %v, want supplied values plus the omitted field's default %v", call.Values, want)
		}
	})
}
