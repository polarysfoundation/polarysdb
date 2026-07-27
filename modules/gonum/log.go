package gonum

import (
    "errors"
    "strings"
)

// lnAtanh computes ln(x) using ln(x)=2*atanh((x-1)/(x+1)).
// Caller must ensure x > 0.
func lnAtanh(x *Number, precision int) *Number {
    workPrecision := precision + 30

    one := One()
    numerator := x.Sub(one)
    denominator := x.Add(one)
    y, _ := numerator.Div(denominator, workPrecision)

    // atanh(y) = y + y^3/3 + y^5/5 + ...
    ySquared := y.Mul(y)
    term := y.Clone()
    sum := y.Clone()

    maxTerms := precision*4 + 400
    if maxTerms < 800 {
        maxTerms = 800
    }

    for i := 1; i < maxTerms; i++ {
        numFactor := FromInt64(2*int64(i) - 1)
        denFactor := FromInt64(2*int64(i) + 1)
        term = term.Mul(ySquared).Mul(numFactor)
        term, _ = term.Div(denFactor, workPrecision)

        if term.IsZero() || cmpMagnitudes(term, Zero()) == 0 {
            break
        }

        termStr := term.scaledDigits(workPrecision)
        if len(termStr) < workPrecision-2 && strings.HasPrefix(termStr, "0") {
            zeros := 0
            for zeros < len(termStr) && termStr[zeros] == '0' {
                zeros++
            }
            if zeros > precision {
                break
            }
        }

        sum = sum.Add(term)
    }

    return sum.Mul(Two()).Round(precision).SetPrecision(precision)
}

// Ln returns the natural logarithm of n.
func (n *Number) Ln() (*Number, error) {
    if n.IsZero() {
        return nil, errors.New("logarithm of zero")
    }
    if n.IsNegative() {
        return nil, errors.New("logarithm of negative number")
    }
    if n.Equal(One()) {
        return Zero(), nil
    }

    precision := n.precision
    workPrecision := precision + 30

    x := n.Clone().SetPrecision(workPrecision)
    adjustPow2 := 0

    // Reduce x to [0.5, 2] via powers of two.
    for x.GT(Two()) {
        x, _ = x.Div(Two(), workPrecision)
        adjustPow2++
    }
    for x.LT(Half()) {
        x = x.Mul(Two())
        adjustPow2--
    }

    base := lnAtanh(x, precision+10)
    if adjustPow2 != 0 {
        ln2 := lnAtanh(Two().SetPrecision(workPrecision), precision+10)
        adj := ln2.Mul(FromInt(adjustPow2))
        base = base.Add(adj)
    }

    return base.Round(precision).SetPrecision(precision).normalize(), nil
}

// Log returns the logarithm base b of n.
func (n *Number) Log(base *Number) (*Number, error) {
    lnN, err := n.Ln()
    if err != nil {
        return nil, err
    }
    lnBase, err := base.Ln()
    if err != nil {
        return nil, err
    }
    result, _ := lnN.Div(lnBase, n.precision)
    return result.normalize(), nil
}

// Log10 returns the base-10 logarithm of n.
func (n *Number) Log10() (*Number, error) {
    if n.IsZero() {
        return nil, errors.New("logarithm of zero")
    }
    if n.IsNegative() {
        return nil, errors.New("logarithm of negative number")
    }

    lnN, err := n.Ln()
    if err != nil {
        return nil, err
    }

    ln10, err := FromInt64(10).SetPrecision(n.precision + 30).Ln()
    if err != nil {
        return nil, err
    }

    result, _ := lnN.Div(ln10, n.precision)
    return result.normalize(), nil
}

// Log2 returns the base-2 logarithm of n.
func (n *Number) Log2() (*Number, error) {
    if n.IsZero() {
        return nil, errors.New("logarithm of zero")
    }
    if n.IsNegative() {
        return nil, errors.New("logarithm of negative number")
    }

    lnN, err := n.Ln()
    if err != nil {
        return nil, err
    }

    ln2, err := FromInt64(2).SetPrecision(n.precision + 30).Ln()
    if err != nil {
        return nil, err
    }

    result, _ := lnN.Div(ln2, n.precision)
    return result.normalize(), nil
}

// Exp returns e^n (e raised to the power n).
func (n *Number) Exp() *Number {
    precision := n.precision
    workPrecision := precision + 25

    // For large |n|, use: e^n = (e^(n/2))^2
    absN := n.Abs()
    if absN.GT(FromInt64(100)) {
        half := n.Clone()
        half, _ = half.Div(Two(), workPrecision)
        halfExp := half.Exp()
        return halfExp.Mul(halfExp).Round(precision).normalize()
    }

    if absN.GT(FromInt64(10)) {
        half := n.Clone()
        half, _ = half.Div(Two(), workPrecision)
        halfExp := half.Exp()
        return halfExp.Mul(halfExp).Round(precision).normalize()
    }

    // Taylor series: e^x = 1 + x + x²/2! + x³/3! + ...
    term := One()
    sum := One()

    maxTerms := precision*3 + 200
    if maxTerms < 400 {
        maxTerms = 400
    }

    for i := 1; i < maxTerms; i++ {
        // term = term * x / i
        term, _ = term.Mul(n).Div(FromInt64(int64(i)), workPrecision)

        if term.IsZero() {
            break
        }

        sum = sum.Add(term)
    }

    return sum.Round(precision).normalize()
}

// Expm1 returns e^n - 1, more accurate for small n.
func (n *Number) Expm1() *Number {
    return n.Exp().Sub(One())
}

// Log1p returns ln(1 + n), more accurate for small n.
func (n *Number) Log1p() (*Number, error) {
    return One().Add(n).Ln()
}
