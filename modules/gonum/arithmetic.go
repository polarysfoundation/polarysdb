package gonum

import (
    "errors"
    "strings"
)

// Add returns the sum of n and other.
func (n *Number) Add(other *Number) *Number {
    precision := imax(n.precision, other.precision)

    if n.negative == other.negative {
        // Same sign: add magnitudes, keep sign
        result := addMagnitudes(n, other, precision)
        result.negative = n.negative
        return result.normalize()
    }

    // Different signs: subtract smaller magnitude from larger
    cmp := cmpMagnitudes(n, other)
    if cmp == 0 {
        return &Number{digits: "0", scale: 0, precision: precision}
    }

    var result *Number
    if cmp > 0 {
        result = subMagnitudes(n, other, precision)
        result.negative = n.negative
    } else {
        result = subMagnitudes(other, n, precision)
        result.negative = other.negative
    }

    return result.normalize()
}

// Sub returns n - other.
func (n *Number) Sub(other *Number) *Number {
    return n.Add(other.Neg())
}

// Mul returns the product of n and other.
func (n *Number) Mul(other *Number) *Number {
    precision := imax(n.precision, other.precision)

    if n.IsZero() || other.IsZero() {
        return &Number{digits: "0", scale: 0, precision: precision}
    }

    // Multiply digit strings
    product := mulStrings(n.digits, other.digits)

    // New scale = sum of scales
    newScale := n.scale + other.scale

    return makeNumber(n.negative != other.negative, product, newScale, precision)
}

// Div returns n / other with the specified precision (or n's precision if prec < 0).
func (n *Number) Div(other *Number, prec ...int) (*Number, error) {
    if other.IsZero() {
        return nil, errors.New("division by zero")
    }

    precision := n.precision
    if len(prec) > 0 && prec[0] >= 0 {
        precision = prec[0]
    }

    if n.IsZero() {
        return &Number{digits: "0", scale: 0, precision: precision}, nil
    }

    // n/other = (n.digits * 10^-n.scale) / (other.digits * 10^-other.scale)
    // To compute with `precision` decimal places, do integer division on:
    //   (n.digits * 10^(precision+other.scale)) / (other.digits * 10^n.scale)
    // and then interpret the quotient with scale=`precision`.
    num := shiftLeft(n.digits, precision+other.scale)
    den := shiftLeft(other.digits, n.scale)

    quotStr, _ := divStrings(num, den, 0)
    return makeNumber(n.negative != other.negative, quotStr, precision, precision).normalize(), nil
}

// MustDiv is like Div but panics on error.
func (n *Number) MustDiv(other *Number, prec ...int) *Number {
    result, err := n.Div(other, prec...)
    if err != nil {
        panic(err)
    }
    return result
}

// Mod returns n % other (modulo).
func (n *Number) Mod(other *Number) (*Number, error) {
    if other.IsZero() {
        return nil, errors.New("modulo by zero")
    }

    // Use: n mod other = n - (n/other)*other
    // But we need integer division for mod
    a := n.Abs()
    b := other.Abs()

    // Scale to make them integers
    targetScale := imax(a.scale, b.scale)
    aScaled := a.scaledDigits(targetScale)
    bScaled := b.scaledDigits(targetScale)

    _, remStr := divModStrings(aScaled, bScaled)

    // Result has the same sign as n
    result := makeNumber(false, remStr, targetScale, imax(n.precision, other.precision))
    if n.IsNegative() && !result.IsZero() {
        result.negative = true
    }

    return result, nil
}

// MustMod is like Mod but panics on error.
func (n *Number) MustMod(other *Number) *Number {
    result, err := n.Mod(other)
    if err != nil {
        panic(err)
    }
    return result
}

// QuoRem returns both quotient and remainder.
func (n *Number) QuoRem(other *Number) (quo *Number, rem *Number, err error) {
    if other.IsZero() {
        return nil, nil, errors.New("division by zero")
    }

    precision := imax(n.precision, other.precision)
    targetScale := imax(n.scale, other.scale)

    aScaled := n.Abs().scaledDigits(targetScale)
    bScaled := other.Abs().scaledDigits(targetScale)

    quotStr, remStr := divModStrings(aScaled, bScaled)

    quo = makeNumber(n.negative != other.negative, quotStr, 0, precision)
    rem = makeNumber(false, remStr, targetScale, precision)
    if n.IsNegative() && !rem.IsZero() {
        rem.negative = true
    }

    return quo, rem, nil
}

// addMagnitudes adds the absolute values of two Numbers.
func addMagnitudes(a, b *Number, precision int) *Number {
    targetScale := imax(a.scale, b.scale)

    aDigits := a.scaledDigits(targetScale)
    bDigits := b.scaledDigits(targetScale)

    sum := addStrings(aDigits, bDigits)

    return makeNumber(false, sum, targetScale, precision)
}

// subMagnitudes subtracts the absolute value of b from a (assumes |a| >= |b|).
func subMagnitudes(a, b *Number, precision int) *Number {
    targetScale := imax(a.scale, b.scale)

    aDigits := a.scaledDigits(targetScale)
    bDigits := b.scaledDigits(targetScale)

    diff := subStrings(aDigits, bDigits)

    return makeNumber(false, diff, targetScale, precision)
}

// cmpMagnitudes compares the absolute values of two Numbers.
// Returns -1 if |a| < |b|, 0 if equal, 1 if |a| > |b|.
func cmpMagnitudes(a, b *Number) int {
    targetScale := imax(a.scale, b.scale)

    aDigits := a.scaledDigits(targetScale)
    bDigits := b.scaledDigits(targetScale)

    return cmpStrings(aDigits, bDigits)
}

// PowInt returns n raised to an integer power.
func (n *Number) PowInt(exp int) *Number {
    if exp == 0 {
        return One()
    }
    if exp < 0 {
        result, _ := One().Div(n, n.precision)
        return result.PowInt(-exp)
    }

    result := One()
    base := n.Clone()

    for exp > 0 {
        if exp%2 == 1 {
            result = result.Mul(base)
        }
        base = base.Mul(base)
        exp /= 2
    }

    return result
}

// SqrtInt returns the integer square root (largest integer <= sqrt(n)).
// Only works for non-negative numbers.
func (n *Number) SqrtInt() *Number {
    if n.IsNegative() {
        return Zero()
    }
    if n.IsZero() {
        return Zero()
    }

    // Convert to integer (truncate decimal)
    intDigits := n.digits
    if n.scale > 0 {
        intDigits = n.digits[:len(n.digits)-n.scale]
    }
    intDigits = removeLeadingZeros(intDigits)
    if intDigits == "" {
        return Zero()
    }

    // Initial guess using number of digits
    guess := "1" + strings.Repeat("0", (len(intDigits)+1)/2)

    // Newton's method: x_{n+1} = (x_n + a/x_n) / 2
    for {
        quot, _ := divStrings(intDigits, guess, 0)
        newGuess, _ := divStrings(addStrings(guess, quot), "2", 0)

        if cmpStrings(guess, newGuess) <= 0 {
            break
        }
        guess = newGuess
    }

    result := FromString(guess)
    return result
}
