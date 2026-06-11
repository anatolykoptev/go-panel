package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anatolykoptev/go-panel/tenant"
)

// FieldKind is the type of form field.
type FieldKind int

const (
	// FieldText is a single-line text input.
	FieldText FieldKind = iota
	// FieldTextarea is a multi-line text area.
	FieldTextarea
	// FieldNumber is a numeric input.
	FieldNumber
	// FieldDate is a date picker (YYYY-MM-DD).
	FieldDate
	// FieldSelect is a dropdown with a closed set of options.
	FieldSelect
	// FieldCheckbox is a boolean checkbox.
	FieldCheckbox
	// FieldJSON is a textarea with json.Valid validation.
	FieldJSON
)

// Option is a single option for a FieldSelect field.
type Option struct {
	Value string
	Label string
}

// Field declares one form field.
type Field struct {
	Key         string
	Label       string
	Kind        FieldKind
	Required    bool
	Options     []Option // required for FieldSelect
	Placeholder string
	Help        string   // optional helper text shown below the field
}

// FormSpec declares the form structure for a Writer.
type FormSpec struct {
	Fields []Field
}

// Valid reports whether the FormSpec is correctly configured.
// Returns a non-nil error for: duplicate keys, empty Key/Label, FieldSelect without Options.
func (f FormSpec) Valid() error {
	seen := make(map[string]bool, len(f.Fields))
	for i, fld := range f.Fields {
		if fld.Key == "" {
			return fmt.Errorf("resource.FormSpec: field[%d] has empty Key", i)
		}
		if fld.Label == "" {
			return fmt.Errorf("resource.FormSpec: field %q has empty Label", fld.Key)
		}
		if seen[fld.Key] {
			return fmt.Errorf("resource.FormSpec: duplicate field Key %q", fld.Key)
		}
		seen[fld.Key] = true
		if fld.Kind == FieldSelect && len(fld.Options) == 0 {
			return fmt.Errorf("resource.FormSpec: field %q is FieldSelect but has no Options", fld.Key)
		}
	}
	return nil
}

// Writer enables create/edit forms for the resource.
// Nil = read-only (Phase 1 behaviour, default).
type Writer struct {
	Form FormSpec
	// Load returns field values for the edit form. id is the row primary key.
	Load func(ctx context.Context, t tenant.Tenant, id string) (map[string]string, error)
	// Save persists the form. id=="" means create.
	// values are PRE-VALIDATED against Form (required, kind formats, select whitelist).
	// Consumer maps them to SQL.
	Save func(ctx context.Context, t tenant.Tenant, id string, values map[string]string) error
	// WriteAny allows any authenticated operator to write.
	WriteAny bool
}

// formErrors holds per-field validation errors.
type formErrors map[string]string

// validate validates posted form values against the FormSpec.
// Returns formErrors (may be empty on success).
func (fs FormSpec) validate(values map[string]string) formErrors {
	errs := make(formErrors)
	for _, fld := range fs.Fields {
		val := values[fld.Key]
		if fld.Required && val == "" {
			errs[fld.Key] = fld.Label + " is required"
			continue
		}
		if val == "" {
			continue
		}
		switch fld.Kind {
		case FieldNumber:
			// Accept integers and decimals.
			var f float64
			if err := json.Unmarshal([]byte(val), &f); err != nil {
				errs[fld.Key] = fld.Label + " must be a number"
			}
		case FieldSelect:
			allowed := false
			for _, opt := range fld.Options {
				if opt.Value == val {
					allowed = true
					break
				}
			}
			if !allowed {
				errs[fld.Key] = fld.Label + " is not a valid choice"
			}
		case FieldJSON:
			if !json.Valid([]byte(val)) {
				errs[fld.Key] = fld.Label + " must be valid JSON"
			}
		case FieldCheckbox:
			if val != "true" && val != "false" && val != "on" && val != "1" && val != "0" && val != "" {
				errs[fld.Key] = fld.Label + " must be a boolean"
			}
		}
	}
	return errs
}

// hasErrors reports whether any field errors exist.
func (fe formErrors) hasErrors() bool {
	return len(fe) > 0
}

// errorsFor is a helper used in templates.
func (fe formErrors) get(key string) string {
	return fe[key]
}

// ErrFormValidation is returned when form validation fails.
// Callers should re-render the form with the validation errors.
var ErrFormValidation = errors.New("form validation failed")
