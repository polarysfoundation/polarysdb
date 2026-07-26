package gonum

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON stores Number as a JSON string to preserve precision.
func (n Number) MarshalJSON() ([]byte, error) {
	return json.Marshal(n.String())
}

// UnmarshalJSON restores Number from either JSON string or number.
func (n *Number) UnmarshalJSON(data []byte) error {
	if n == nil {
		return fmt.Errorf("gonum.Number: nil receiver")
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		parsed := New(s)
		if parsed == nil {
			return fmt.Errorf("gonum.Number: invalid number %q", s)
		}
		*n = *parsed
		return nil
	}

	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("gonum.Number: invalid JSON value: %w", err)
	}

	parsed := New(fmt.Sprintf("%.17g", f))
	if parsed == nil {
		return fmt.Errorf("gonum.Number: unable to parse numeric value")
	}
	*n = *parsed
	return nil
}
