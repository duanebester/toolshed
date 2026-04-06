// Validation logic for registry types.
package core

import (
	"encoding/json"
	"fmt"
)

// ValidateInput checks that the provided input map satisfies every field
// defined in schema.Input.  Extra keys in input are silently ignored.
func ValidateInput(schema Schema, input map[string]any) error {
	return validateFields(schema.Input, input, "input")
}

// ValidateOutput checks that the provided output map satisfies every field
// defined in schema.Output.  Extra keys in output are silently ignored.
func ValidateOutput(schema Schema, output map[string]any) error {
	return validateFields(schema.Output, output, "output")
}

// validateFields is the shared implementation used by both ValidateInput and
// ValidateOutput.  label is a human-readable word ("input" / "output") that
// is included in error messages for context.
func validateFields(fields map[string]FieldDef, data map[string]any, label string) error {
	for name, def := range fields {
		val, ok := data[name]
		if !ok {
			return fmt.Errorf("%s field %q is required but missing", label, name)
		}
		if err := validateValue(name, def, val, label); err != nil {
			return err
		}
	}
	return nil
}

// validateValue validates a single value against a FieldDef.
func validateValue(name string, def FieldDef, val any, label string) error {
	switch def.Type {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("%s field %q: expected string, got %T", label, name, val)
		}

	case "number":
		n, err := toFloat64(val)
		if err != nil {
			return fmt.Errorf("%s field %q: expected number, got %T", label, name, val)
		}
		if def.Min != nil && n < *def.Min {
			return fmt.Errorf("%s field %q: value %v is less than minimum %v", label, name, n, *def.Min)
		}
		if def.Max != nil && n > *def.Max {
			return fmt.Errorf("%s field %q: value %v is greater than maximum %v", label, name, n, *def.Max)
		}

	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("%s field %q: expected boolean, got %T", label, name, val)
		}

	case "array":
		arr, ok := val.([]any)
		if !ok {
			return fmt.Errorf("%s field %q: expected array, got %T", label, name, val)
		}
		if def.Items != nil {
			for i, elem := range arr {
				elemName := fmt.Sprintf("%s[%d]", name, i)
				if err := validateValue(elemName, *def.Items, elem, label); err != nil {
					return err
				}
			}
		}

	case "object":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("%s field %q: expected object, got %T", label, name, val)
		}

	default:
		return fmt.Errorf("%s field %q: unknown type %q in schema", label, name, def.Type)
	}

	return nil
}

// toFloat64 converts a value to float64, accepting float64, int, and
// json.Number (which appears when decoding with UseNumber).
func toFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}
