package gonum

import (
	"strconv"
	"strings"
)

// DefaultPrecision is the default number of decimal places for calculations.
var DefaultPrecision = 0

// Number represents an arbitrary-precision decimal number.
// Stored as: sign + digits string + scale (decimal places count).
//
// Examples:
//
//	"123.456" → digits="123456", scale=3
//	"-42"     → negative=true, digits="42", scale=0
//	"0.001"   → digits="1", scale=3
type Number struct {
	negative  bool
	digits    string // significant digits, no leading/trailing zeros (except "0")
	scale     int    // number of digits after decimal point
	precision int    // precision for calculations
}

// NewNumber creates a new Number from a string representation.
func New(s string) *Number {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}

	if s == "" {
		return nil
	}

	// Handle special strings
	lower := strings.ToLower(s)
	if lower == "nan" || lower == "inf" || lower == "+inf" || lower == "-inf" || lower == "infinity" {
		return nil
	}

	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	decPart := ""
	if len(parts) == 2 {
		decPart = parts[1]
	}

	if intPart == "" {
		intPart = "0"
	}

	// Validate all characters
	for _, c := range intPart {
		if c < '0' || c > '9' {
			return nil
		}
	}
	for _, c := range decPart {
		if c < '0' || c > '9' {
			return nil
		}
	}

	// Normalize
	intPart = removeLeadingZeros(intPart)
	if intPart == "" {
		intPart = "0"
	}
	decPart = removeTrailingZeros(decPart)

	digits := intPart + decPart
	scale := len(decPart)

	if digits == "0" {
		scale = 0
		neg = false
	}

	return &Number{
		negative:  neg,
		digits:    digits,
		scale:     scale,
		precision: DefaultPrecision,
	}
}

// FromInt64 creates a Number from an int64.
func FromInt64(n int64) *Number {
	if n == 0 {
		return &Number{digits: "0", scale: 0, precision: DefaultPrecision}
	}
	neg := n < 0
	if neg {
		n = -n
	}
	return &Number{
		negative:  neg,
		digits:    strconv.FormatInt(n, 10),
		scale:     0,
		precision: DefaultPrecision,
	}
}

// FromUint64 creates a Number from a uint64.
func FromUint64(n uint64) *Number {
	if n == 0 {
		return &Number{digits: "0", scale: 0, precision: DefaultPrecision}
	}
	return &Number{
		negative:  false,
		digits:    strconv.FormatUint(n, 10),
		scale:     0,
		precision: DefaultPrecision,
	}
}

// FromInt is an alias for FromInt64.
func FromInt(n int) *Number {
	return FromInt64(int64(n))
}

// FromFloat64 creates a Number from a float64 with given precision.
func FromFloat64(f float64, precision int) *Number {
	if precision < 0 {
		precision = 0
	}
	s := strconv.FormatFloat(f, 'f', precision, 64)
	n := New(s)
	if n != nil {
		n.precision = precision
	}
	return n
}

// FromBytes creates a Number from a big-endian byte slice.
func FromBytes(b []byte) *Number {
	if len(b) == 0 {
		return Zero()
	}

	result := Zero()
	for _, byteVal := range b {
		result = result.Mul(FromInt64(256))
		result = result.Add(FromInt64(int64(byteVal)))
	}
	return result
}

// ToBytes returns the Number as a big-endian byte slice.
func (n *Number) ToBytes() []byte {
	if n.IsZero() {
		return []byte{0}
	}

	intVal, err := n.Int64()
	if err == nil {
		if intVal == 0 {
			return []byte{0}
		}
		result := make([]byte, 0, 8)
		for intVal > 0 {
			result = append([]byte{byte(intVal & 0xff)}, result...)
			intVal >>= 8
		}
		return result
	}

	// For numbers that don't fit in int64, convert to bytes via string
	s := n.String()
	return []byte(s)
}

// FromString is an alias for NewNumber.
func FromString(s string) *Number {
	return New(s)
}

// String returns the standard string representation of the Number.
func (n *Number) String() string {
	if n.digits == "0" {
		return "0"
	}

	sign := ""
	if n.negative {
		sign = "-"
	}

	intLen := len(n.digits) - n.scale

	if intLen <= 0 {
		return sign + "0." + strings.Repeat("0", -intLen) + n.digits
	}

	intPart := n.digits[:intLen]
	decPart := n.digits[intLen:]

	if decPart == "" {
		return sign + intPart
	}

	return sign + intPart + "." + decPart
}

// SetPrecision sets the precision for calculations and returns the Number for chaining.
func (n *Number) SetPrecision(p int) *Number {
	if p < 0 {
		p = 0
	}
	n.precision = p
	return n
}

// GetPrecision returns the current precision setting.
func (n *Number) GetPrecision() int {
	return n.precision
}

// IsZero returns true if the number is zero.
func (n *Number) IsZero() bool {
	return n.digits == "0"
}

// IsNegative returns true if the number is negative.
func (n *Number) IsNegative() bool {
	return n.negative && !n.IsZero()
}

// IsPositive returns true if the number is positive.
func (n *Number) IsPositive() bool {
	return !n.negative && !n.IsZero()
}

// Sign returns -1, 0, or 1 depending on the sign of the number.
func (n *Number) Sign() int {
	if n.IsZero() {
		return 0
	}
	if n.negative {
		return -1
	}
	return 1
}

// Clone returns a deep copy of the Number.
func (n *Number) Clone() *Number {
	return &Number{
		negative:  n.negative,
		digits:    n.digits,
		scale:     n.scale,
		precision: n.precision,
	}
}

// Neg returns the negation of the number.
func (n *Number) Neg() *Number {
	result := n.Clone()
	if !result.IsZero() {
		result.negative = !result.negative
	}
	return result
}

// Abs returns the absolute value of the number.
func (n *Number) Abs() *Number {
	result := n.Clone()
	result.negative = false
	return result
}

// Zero returns a zero Number with default precision.
func Zero() *Number {
	return &Number{digits: "0", scale: 0, precision: DefaultPrecision}
}

// One returns a Number equal to 1.
func One() *Number {
	return &Number{digits: "1", scale: 0, precision: DefaultPrecision}
}

// Two returns a Number equal to 2.
func Two() *Number {
	return &Number{digits: "2", scale: 0, precision: DefaultPrecision}
}

// Ten returns a Number equal to 10.
func Ten() *Number {
	return &Number{digits: "10", scale: 0, precision: DefaultPrecision}
}

// Half returns a Number equal to 0.5.
func Half() *Number {
	return &Number{digits: "5", scale: 1, precision: DefaultPrecision}
}

// normalize removes unnecessary trailing zeros from the decimal part.
func (n *Number) normalize() *Number {
	if n.scale == 0 || n.digits == "0" {
		return n
	}

	decStart := len(n.digits) - n.scale
	newEnd := len(n.digits)
	for newEnd > decStart && n.digits[newEnd-1] == '0' {
		newEnd--
	}

	removedZeros := len(n.digits) - newEnd
	n.digits = n.digits[:newEnd]
	n.scale -= removedZeros

	if n.scale <= 0 {
		n.scale = 0
	}

	if n.digits == "" || n.digits == "0" {
		n.digits = "0"
		n.scale = 0
		n.negative = false
	}

	return n
}

// scaledDigits returns digits padded to have exactly targetScale decimal places.
func (n *Number) scaledDigits(targetScale int) string {
	if n.scale >= targetScale {
		cut := n.scale - targetScale
		return n.digits[:len(n.digits)-cut]
	}
	return n.digits + strings.Repeat("0", targetScale-n.scale)
}

// Int64 converts the Number to int64, truncating any decimal part.
// Returns error if the number doesn't fit in int64.
func (n *Number) Int64() (int64, error) {
	if n.scale > 0 {
		n = New(n.digits[:len(n.digits)-n.scale])
	}
	v, err := strconv.ParseInt(n.digits, 10, 64)
	if err != nil {
		return 0, err
	}
	if n.negative && v != 0 {
		return -v, nil
	}
	return v, nil
}

// Float64 converts the Number to float64 (may lose precision).
func (n *Number) Float64() (float64, error) {
	return strconv.ParseFloat(n.String(), 64)
}

// makeNumber creates a normalized Number from raw components.
func makeNumber(neg bool, digits string, scale int, precision int) *Number {
	digits = removeLeadingZeros(digits)
	if digits == "" {
		digits = "0"
	}

	// Ensure we have enough digits to represent the requested scale.
	// If scale is larger than the digit count, pad on the left (e.g. digits="1", scale=3 => "0001", scale=3 => 0.001).
	if scale > len(digits) {
		digits = strings.Repeat("0", scale-len(digits)) + digits
	}

	// Trim trailing zeros that are entirely in the fractional part.
	if scale > 0 {
		cut := len(digits)
		decStart := len(digits) - scale
		for cut > decStart && digits[cut-1] == '0' {
			cut--
		}
		if cut != len(digits) {
			removed := len(digits) - cut
			digits = digits[:cut]
			scale -= removed
		}
		if scale < 0 {
			scale = 0
		}
	}

	digits = removeLeadingZeros(digits)
	if digits == "" || digits == "0" {
		return &Number{digits: "0", scale: 0, precision: precision}
	}

	if precision < 0 {
		precision = DefaultPrecision
	}

	return &Number{
		negative:  neg && digits != "0",
		digits:    digits,
		scale:     scale,
		precision: precision,
	}
}
