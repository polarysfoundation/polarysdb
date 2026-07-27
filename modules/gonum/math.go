package gonum

import (
	"errors"
	"strings"
)

// Sqrt returns the square root of n using Newton's method.
func (n *Number) Sqrt() (*Number, error) {
	if n.IsNegative() {
		return nil, errors.New("square root of negative number")
	}
	if n.IsZero() {
		return Zero(), nil
	}
	if n.Equal(One()) {
		return One(), nil
	}

	precision := n.precision
	extra := 10
	workPrecision := precision + extra

	// We compute sqrt(n) by converting n into an integer:
	//   n = (digits * 10^-scale)
	// Choose an exponent k such that digits*10^(k-scale) is an integer with an even exponent,
	// so we can take an integer square root and then rescale.
	//
	// Let k = 2*workPrecision. Define:
	//   A = digits * 10^(k - scale)
	// Then:
	//   sqrt(n) = sqrt(A) * 10^(-k/2)
	k := 2 * workPrecision
	shift := k - n.scale
	if shift < 0 {
		// If the input has more decimals than our working precision, truncate.
		// (This is conservative; higher workPrecision reduces this case.)
		trunc := -shift
		if trunc >= len(n.digits) {
			return Zero(), nil
		}
		aDigits := n.digits[:len(n.digits)-trunc]
		shift = 0
		n = makeNumber(n.negative, aDigits, 0, n.precision)
	}

	aScaled := shiftLeft(n.digits, shift)
	if aScaled == "" || aScaled == "0" {
		return Zero(), nil
	}

	// Integer square root via Newton iteration on strings.
	guessDigits := (len(aScaled) + 1) / 2
	guess := "1" + strings.Repeat("0", guessDigits-1)

	prev := ""
	for i := 0; i < 2000; i++ {
		quot, _ := divStrings(aScaled, guess, 0)
		newGuess, _ := divStrings(addStrings(guess, quot), "2", 0)

		if cmpStrings(guess, newGuess) == 0 {
			break
		}
		if prev != "" && strings.HasPrefix(newGuess, prev) {
			break
		}
		prev = guess
		guess = newGuess
	}

	// Rescale by k/2 decimal places.
	result := makeNumber(false, guess, k/2, precision).Round(precision).SetPrecision(precision)
	return result, nil
}

// MustSqrt is like Sqrt but panics on error.
func (n *Number) MustSqrt() *Number {
	result, err := n.Sqrt()
	if err != nil {
		panic(err)
	}
	return result
}

// Pow returns n raised to the power of exp (can be fractional).
func (n *Number) Pow(exp *Number) (*Number, error) {

	// Handle integer exponents efficiently
	if exp.IsInteger() {
		expInt, _ := exp.Int64()
		return n.PowInt(int(expInt)), nil
	}

	// For fractional exponents: n^exp = exp(exp * ln(n))
	if n.IsZero() {
		if exp.IsPositive() {
			return Zero(), nil
		}
		return nil, errors.New("0 raised to a non-positive power")
	}

	if n.IsNegative() {
		return nil, errors.New("negative base with fractional exponent")
	}

	lnN, err := n.Ln()
	if err != nil {
		return nil, err
	}

	product := lnN.Mul(exp)
	return product.Exp(), nil
}

// MustPow is like Pow but panics on error.
func (n *Number) MustPow(exp *Number) *Number {
	result, err := n.Pow(exp)
	if err != nil {
		panic(err)
	}
	return result
}

// Round rounds the number to the given number of decimal places.
func (n *Number) Round(places int) *Number {
	if places < 0 {
		places = 0
	}

	if n.scale <= places {
		return n.Clone()
	}

	// Keep exactly `places` decimal digits, and look at the next digit for rounding.
	cutPos := len(n.digits) - (n.scale - places)
	if cutPos <= 0 {
		return Zero()
	}

	digitsToKeep := n.digits[:cutPos]
	roundDigit := n.digits[cutPos]

	// Round half up
	if roundDigit >= '5' {
		digitsToKeep = addStrings(digitsToKeep, "1")
	}

	return makeNumber(n.negative, digitsToKeep, places, n.precision)
}

// Floor returns the largest integer less than or equal to n.
func (n *Number) Floor() *Number {
	if n.scale == 0 {
		return n.Clone()
	}

	if n.IsNegative() {
		// For negative numbers, floor goes more negative if there's a decimal part
		intPart := n.digits[:len(n.digits)-n.scale]
		decPart := n.digits[len(n.digits)-n.scale:]

		hasDecimal := false
		for _, c := range decPart {
			if c != '0' {
				hasDecimal = true
				break
			}
		}

		if hasDecimal {
			intPart = addStrings(intPart, "1")
		}

		return makeNumber(true, intPart, 0, n.precision)
	}

	// Positive: just truncate
	intPart := n.digits[:len(n.digits)-n.scale]
	return makeNumber(false, intPart, 0, n.precision)
}

// Ceil returns the smallest integer greater than or equal to n.
func (n *Number) Ceil() *Number {
	if n.scale == 0 {
		return n.Clone()
	}

	if n.IsPositive() {
		// For positive numbers, ceil goes more positive if there's a decimal part
		intPart := n.digits[:len(n.digits)-n.scale]
		decPart := n.digits[len(n.digits)-n.scale:]

		hasDecimal := false
		for _, c := range decPart {
			if c != '0' {
				hasDecimal = true
				break
			}
		}

		if hasDecimal {
			intPart = addStrings(intPart, "1")
		}

		return makeNumber(false, intPart, 0, n.precision)
	}

	// Negative: just truncate
	intPart := n.digits[:len(n.digits)-n.scale]
	return makeNumber(true, intPart, 0, n.precision)
}

// Trunc returns the integer part of n (truncates toward zero).
func (n *Number) Trunc() *Number {
	if n.scale == 0 {
		return n.Clone()
	}

	intPart := n.digits[:len(n.digits)-n.scale]
	return makeNumber(n.negative, intPart, 0, n.precision)
}

// Frac returns the fractional part of n.
func (n *Number) Frac() *Number {
	if n.scale == 0 {
		return Zero()
	}

	decPart := n.digits[len(n.digits)-n.scale:]
	decPart = removeTrailingZeros(decPart)
	if decPart == "" {
		return Zero()
	}

	return makeNumber(n.negative, decPart, len(decPart), n.precision)
}

// Hypot returns sqrt(a^2 + b^2).
func Hypot(a, b *Number) *Number {
	a2 := a.Mul(a)
	b2 := b.Mul(b)
	sum := a2.Add(b2)
	result, _ := sum.Sqrt()
	return result
}

// Signum returns -1, 0, or 1 based on the sign of n.
func (n *Number) Signum() *Number {
	return FromInt(n.Sign())
}

// Factorial returns n! for non-negative integer n.
func Factorial(n *Number) (*Number, error) {
	if n.IsNegative() {
		return nil, errors.New("factorial of negative number")
	}
	if !n.IsInteger() {
		return nil, errors.New("factorial requires integer input")
	}

	intVal, err := n.Int64()
	if err != nil {
		return nil, errors.New("number too large for factorial")
	}

	result := One()
	for i := int64(2); i <= intVal; i++ {
		result = result.Mul(FromInt64(i))
	}

	return result, nil
}

// GCD returns the greatest common divisor of a and b.
func GCD(a, b *Number) *Number {
	a = a.Abs().Clone()
	b = b.Abs().Clone()

	for !b.IsZero() {
		rem, _ := a.Mod(b)
		a = b
		b = rem
	}

	return a
}

// LCM returns the least common multiple of a and b.
func LCM(a, b *Number) *Number {
	if a.IsZero() || b.IsZero() {
		return Zero()
	}
	gcd := GCD(a, b)
	result, _ := a.Mul(b).Div(gcd)
	return result
}

// Quo returns the quotient of a divided by b with specified precision.
func (n *Number) Quo(b *Number) (*Number, error) {
	return n.Div(b, n.precision)
}
