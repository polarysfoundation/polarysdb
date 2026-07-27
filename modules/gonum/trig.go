package gonum

import (
    "errors"
)

// Sin returns the sine of n (n in radians).
func (n *Number) Sin() *Number {
    precision := n.precision
    workPrecision := precision + 10

    // Reduce to range [-π, π]
    pi := Pi(workPrecision)
    twoPi := pi.Mul(Two())
    reduced, _ := n.Mod(twoPi)

    // Adjust for negative
    if reduced.IsNegative() {
        reduced = reduced.Add(twoPi)
    }

    // Now reduced is in [0, 2π)
    // Further reduce to [-π, π]
    if reduced.Cmp(pi) > 0 {
        reduced = reduced.Sub(twoPi)
    }

    // Use Taylor series: sin(x) = x - x³/3! + x⁵/5! - x⁷/7! + ...
    term := reduced.Clone()
    sum := reduced.Clone()
    xSquared := reduced.Mul(reduced)
    maxTerms := precision*2 + 50
    if maxTerms < 100 {
        maxTerms = 100
    }

    for i := 1; i < maxTerms; i++ {
        // term = term * (-x²) / ((2i)(2i+1))
        denom1 := FromInt64(2 * int64(i))
        denom2 := FromInt64(2*int64(i) + 1)
        denom := denom1.Mul(denom2)

        term, _ = term.Mul(xSquared).Div(denom, workPrecision)
        term.negative = !term.negative

        // Check if term is small enough
        if term.IsZero() {
            break
        }

        sum = sum.Add(term)
    }

    return sum.Round(precision).normalize()
}

// Cos returns the cosine of n (n in radians).
func (n *Number) Cos() *Number {
    precision := n.precision
    workPrecision := precision + 10

    // Reduce to range [-π, π]
    pi := Pi(workPrecision)
    twoPi := pi.Mul(Two())
    reduced, _ := n.Mod(twoPi)

    if reduced.IsNegative() {
        reduced = reduced.Add(twoPi)
    }

    if reduced.Cmp(pi) > 0 {
        reduced = reduced.Sub(twoPi)
    }

    // Use Taylor series: cos(x) = 1 - x²/2! + x⁴/4! - x⁶/6! + ...
    term := One()
    sum := One()
    xSquared := reduced.Mul(reduced)

    maxTerms := precision*2 + 50
    if maxTerms < 100 {
        maxTerms = 100
    }

    for i := 1; i < maxTerms; i++ {
        // term = term * (-x²) / ((2i-1)(2i))
        denom1 := FromInt64(2*int64(i) - 1)
        denom2 := FromInt64(2 * int64(i))
        denom := denom1.Mul(denom2)

        term, _ = term.Mul(xSquared).Div(denom, workPrecision)
        term.negative = !term.negative

        if term.IsZero() {
            break
        }

        sum = sum.Add(term)
    }

    return sum.Round(precision).normalize()
}

// Tan returns the tangent of n (n in radians).
func (n *Number) Tan() *Number {
    sinVal := n.Sin()
    cosVal := n.Cos()
    result, _ := sinVal.Div(cosVal, n.precision)
    return result.normalize()
}

// ASin returns the arcsine of n (result in radians).
// n must be in range [-1, 1].
func (n *Number) ASin() (*Number, error) {
    one := One()
    if n.LT(one.Neg()) || n.GT(one) {
        return nil, errors.New("arcsin: input out of range [-1, 1]")
    }

    if n.Equal(one) {
        piHalf, _ := Pi(n.precision).Div(Two(), n.precision)
        return piHalf, nil // π/2
    }
    if n.Equal(one.Neg()) {
        pi, _ := Pi(n.precision).Div(Two(), n.precision)
        return pi.Neg(), nil // -π/2
    }
    if n.IsZero() {
        return Zero(), nil
    }

    precision := n.precision
    workPrecision := precision + 10

    // Use: arcsin(x) = arctan(x / sqrt(1 - x²))
    oneMinusX2 := One().Sub(n.Mul(n))
    sqrtVal, _ := oneMinusX2.Sqrt()
    dividend := n.Clone()
    divisor := sqrtVal
    result, _ := dividend.Div(divisor, workPrecision)
    return result.ATan(), nil
}

// ACos returns the arccosine of n (result in radians).
// n must be in range [-1, 1].
func (n *Number) ACos() (*Number, error) {
    asin, err := n.ASin()
    if err != nil {
        return nil, err
    }
    // arccos(x) = π/2 - arcsin(x)
    piHalf, _ := Pi(n.precision).Div(Two(), n.precision)
    return piHalf.Sub(asin), nil
}

// ATan returns the arctangent of n (result in radians).
func (n *Number) ATan() *Number {
    precision := n.precision
    workPrecision := precision + 10

    absN := n.Abs()
    negate := n.IsNegative()

    // For |x| > 1, use: atan(x) = π/2 - atan(1/x)
    if absN.GT(One()) {
        reciprocal, _ := One().Div(absN, workPrecision)
        atanRecip := reciprocal.ATan()
        piHalf, _ := Pi(precision).Div(Two(), precision)
        result := piHalf.Sub(atanRecip)
        if negate {
            result = result.Neg()
        }
        return result.normalize()
    }

    // For |x| > 0.5, use: atan(x) = 2*atan(x / (1 + sqrt(1 + x²)))
    if absN.GT(Half()) {
        xSquared := absN.Mul(absN)
        onePlusX2 := One().Add(xSquared)
        sqrtVal, _ := onePlusX2.Sqrt()
        denom := One().Add(sqrtVal)
        reduced, _ := absN.Div(denom, workPrecision)
        result := reduced.ATan().Mul(Two())
        if negate {
            result = result.Neg()
        }
        return result.normalize()
    }

    // Taylor series: atan(x) = x - x³/3 + x⁵/5 - x⁷/7 + ...
    term := absN.Clone()
    sum := absN.Clone()
    xSquared := absN.Mul(absN)

    maxTerms := precision*2 + 50
    if maxTerms < 100 {
        maxTerms = 100
    }

    for i := 1; i < maxTerms; i++ {
        numFactor := FromInt64(2*int64(i) - 1)
        denFactor := FromInt64(2*int64(i) + 1)
        term = term.Mul(xSquared).Mul(numFactor)
        term, _ = term.Div(denFactor, workPrecision)
        term.negative = !term.negative

        if term.IsZero() {
            break
        }

        sum = sum.Add(term)
    }

    if negate {
        sum = sum.Neg()
    }

    return sum.Round(precision).normalize()
}

// SinDeg returns the sine of n degrees.
func (n *Number) SinDeg() *Number {
    radians := n.DegToRad()
    return radians.Sin()
}

// CosDeg returns the cosine of n degrees.
func (n *Number) CosDeg() *Number {
    radians := n.DegToRad()
    return radians.Cos()
}

// TanDeg returns the tangent of n degrees.
func (n *Number) TanDeg() *Number {
    radians := n.DegToRad()
    return radians.Tan()
}

// DegToRad converts degrees to radians.
func (n *Number) DegToRad() *Number {
    pi := Pi(n.precision)
    factor, _ := pi.Div(FromInt64(180), n.precision)
    return n.Mul(factor)
}

// RadToDeg converts radians to degrees.
func (n *Number) RadToDeg() *Number {
    pi := Pi(n.precision)
    factor, _ := FromInt64(180).Div(pi, n.precision)
    return n.Mul(factor)
}

// SinH returns the hyperbolic sine of n.
func (n *Number) SinH() *Number {
    // sinh(x) = (e^x - e^-x) / 2
    posExp := n.Exp()
    negExp := n.Neg().Exp()
    diff := posExp.Sub(negExp)
    result, _ := diff.Div(Two(), n.precision)
    return result
}

// CosH returns the hyperbolic cosine of n.
func (n *Number) CosH() *Number {
    // cosh(x) = (e^x + e^-x) / 2
    posExp := n.Exp()
    negExp := n.Neg().Exp()
    sum := posExp.Add(negExp)
    result, _ := sum.Div(Two(), n.precision)
    return result
}

// TanH returns the hyperbolic tangent of n.
func (n *Number) TanH() *Number {
    sinh := n.SinH()
    cosh := n.CosH()
    result, _ := sinh.Div(cosh, n.precision)
    return result
}
