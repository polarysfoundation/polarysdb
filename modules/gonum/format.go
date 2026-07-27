package gonum

import (
	"fmt"
	"strings"
)

// FormatOption specifies formatting options.
type FormatOption struct {
	Prefix       string // e.g., "$"
	Suffix       string // e.g., "%"
	ThousandsSep string // e.g., "," or "."
	DecimalSep   string // e.g., "." or ","
	MinDecimals  int    // minimum decimal places
	MaxDecimals  int    // maximum decimal places (-1 for all)
	ShowSign     bool   // always show + or -
	Padding      int    // total width for padding
	PadChar      byte   // character for padding (default ' ')
	PadLeft      bool   // pad on left (default true)
}

// DefaultFormat returns default formatting options.
func DefaultFormat() FormatOption {
	return FormatOption{
		DecimalSep:   ".",
		ThousandsSep: "",
		MinDecimals:  0,
		MaxDecimals:  -1,
		PadChar:      ' ',
		PadLeft:      true,
	}
}

// Format returns a formatted string representation of the Number.
func (n *Number) Format(opt FormatOption) string {
	s := n.String()

	// Handle sign
	sign := ""
	if s[0] == '-' {
		sign = "-"
		s = s[1:]
	} else if opt.ShowSign {
		sign = "+"
	}

	// Split integer and decimal parts
	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	decPart := ""
	if len(parts) == 2 {
		decPart = parts[1]
	}

	// Add thousands separator
	if opt.ThousandsSep != "" {
		intPart = addThousandsSep(intPart, opt.ThousandsSep)
	}

	// Handle decimal places
	if opt.MaxDecimals >= 0 && len(decPart) > opt.MaxDecimals {
		decPart = decPart[:opt.MaxDecimals]
	}
	if len(decPart) < opt.MinDecimals {
		decPart = decPart + strings.Repeat("0", opt.MinDecimals-len(decPart))
	}

	// Build result
	result := sign + intPart
	if decPart != "" || opt.MinDecimals > 0 {
		result += opt.DecimalSep + decPart
	}

	// Add prefix and suffix
	result = opt.Prefix + result + opt.Suffix

	// Padding
	if opt.Padding > len(result) {
		padLen := opt.Padding - len(result)
		padding := strings.Repeat(string(opt.PadChar), padLen)
		if opt.PadLeft {
			result = padding + result
		} else {
			result = result + padding
		}
	}

	return result
}

// addThousandsSep adds thousands separator to an integer string.
func addThousandsSep(s, sep string) string {
	if len(s) <= 3 {
		return s
	}

	result := make([]byte, 0, len(s)+(len(s)/3))
	count := 0

	for i := len(s) - 1; i >= 0; i-- {
		if count > 0 && count%3 == 0 {
			result = append([]byte(sep), result...)
		}
		result = append([]byte{s[i]}, result...)
		count++
	}

	return string(result)
}

// Fixed returns the number formatted with exactly n decimal places.
func (n *Number) Fixed(places int) string {
	rounded := n.Round(places)
	opt := DefaultFormat()
	opt.MinDecimals = places
	return rounded.Format(opt)
}

// Scientific returns the number in scientific notation.
func (n *Number) Scientific() string {
	if n.IsZero() {
		return "0e+0"
	}

	digits := n.digits
	neg := n.negative
	scale := n.scale

	// Remove leading zeros
	digits = removeLeadingZeros(digits)

	// Calculate exponent
	intLen := len(digits) - scale
	exponent := intLen - 1

	// Build mantissa: first digit . remaining digits
	if len(digits) == 1 {
		if neg {
			return "-" + string(digits[0]) + "e+" + formatInt(exponent)
		}
		return string(digits[0]) + "e+" + formatInt(exponent)
	}

	mantissa := string(digits[0]) + "." + digits[1:]

	if neg {
		mantissa = "-" + mantissa
	}

	expStr := formatInt(exponent)
	if exponent >= 0 {
		expStr = "+" + expStr
	} else {
		expStr = "-" + formatInt(iabs(exponent))
	}

	return mantissa + "e" + expStr
}

// Engineering returns the number in engineering notation (exponent is multiple of 3).
func (n *Number) Engineering() string {
	if n.IsZero() {
		return "0e+0"
	}

	digits := removeLeadingZeros(n.digits)
	neg := n.negative
	scale := n.scale

	intLen := len(digits) - scale
	exponent := intLen - 1

	// Adjust exponent to be multiple of 3
	adjust := exponent % 3
	if adjust < 0 {
		adjust += 3
	}
	exponent -= adjust

	// Mantissa has (adjust + 1) digits before decimal point
	mantissaDigits := adjust + 1
	if mantissaDigits > len(digits) {
		digits = digits + strings.Repeat("0", mantissaDigits-len(digits))
	}

	mantissa := digits[:mantissaDigits] + "." + digits[mantissaDigits:]
	mantissa = removeTrailingZeros(mantissa)

	if before, ok := strings.CutSuffix(mantissa, "."); ok {
		mantissa = before
	}

	if neg {
		mantissa = "-" + mantissa
	}

	expStr := formatInt(exponent)
	if exponent >= 0 {
		expStr = "+" + expStr
	} else {
		expStr = "-" + formatInt(iabs(exponent))
	}

	return mantissa + "e" + expStr
}

// Percent returns the number multiplied by 100 with a "%" suffix.
func (n *Number) Percent(places int) string {
	hundred := FromInt64(100).SetPrecision(n.precision)
	result := n.Mul(hundred).Round(places)
	opt := DefaultFormat()
	opt.MinDecimals = places
	opt.Suffix = "%"
	return result.Format(opt)
}

// Currency returns the number formatted as currency.
func (n *Number) Currency(symbol string, places int) string {
	opt := DefaultFormat()
	opt.Prefix = symbol
	opt.ThousandsSep = ","
	opt.MinDecimals = places
	opt.MaxDecimals = places
	return n.Round(places).Format(opt)
}

// Bytes returns a human-readable byte size string.
func (n *Number) Bytes() string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	current := n.Clone()
	unitIdx := 0
	threshold := FromInt64(1024)

	for current.GTE(threshold) && unitIdx < len(units)-1 {
		var err error
		current, err = current.Div(threshold, 2)
		if err != nil {
			break
		}
		unitIdx++
	}

	return current.Fixed(2) + " " + units[unitIdx]
}

// GoString implements fmt.GoStringer for debugging.
func (n *Number) GoString() string {
	return fmt.Sprintf("&Number{negative: %v, digits: %q, scale: %d, precision: %d}",
		n.negative, n.digits, n.scale, n.precision)
}

// formatInt formats an int as a string without using fmt.
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}

	neg := false
	if n < 0 {
		neg = true
		n = -n
	}

	var digits []byte
	for n > 0 {
		digits = append([]byte{byte(n%10 + '0')}, digits...)
		n /= 10
	}

	if neg {
		digits = append([]byte{'-'}, digits...)
	}

	return string(digits)
}
