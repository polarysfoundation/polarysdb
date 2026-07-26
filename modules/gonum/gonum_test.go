package gonum

import (
	"encoding/json"
	"math"
	"testing"
)

func withDefaultPrecision(t *testing.T, p int, fn func()) {
	t.Helper()
	orig := DefaultPrecision
	DefaultPrecision = p
	t.Cleanup(func() { DefaultPrecision = orig })
	fn()
}

func mustNew(t *testing.T, s string) *Number {
	t.Helper()
	n := New(s)
	if n == nil {
		t.Fatalf("New(%q) returned nil", s)
	}
	return n
}

func mustFloat64(t *testing.T, n *Number) float64 {
	t.Helper()
	f, err := n.Float64()
	if err != nil {
		t.Fatalf("Float64() error: %v (n=%s)", err, n.String())
	}
	return f
}

func assertString(t *testing.T, got *Number, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil, want %q", want)
	}
	if got.String() != want {
		t.Fatalf("got %q, want %q", got.String(), want)
	}
}

func assertAlmost(t *testing.T, got *Number, want, tol float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil, want ~%g", want)
	}
	g := mustFloat64(t, got)
	if math.IsNaN(g) || math.IsInf(g, 0) {
		t.Fatalf("got %v, want ~%g", g, want)
	}
	if math.Abs(g-want) > tol {
		t.Fatalf("got %g, want %g (tol=%g)", g, want, tol)
	}
}

func TestNumber_New_String_Conversions(t *testing.T) {
	withDefaultPrecision(t, 8, func() {
		assertString(t, mustNew(t, "001.2300"), "1.23")
		assertString(t, mustNew(t, "-0.000"), "0")
		assertString(t, mustNew(t, ".5"), "0.5")
		assertString(t, mustNew(t, "5."), "5")
		if New("") != nil {
			t.Fatalf("expected New(\"\") to be nil")
		}
		if New("nan") != nil {
			t.Fatalf("expected New(\"nan\") to be nil")
		}

		if got, err := FromInt64(-42).Int64(); err != nil || got != -42 {
			t.Fatalf("Int64 roundtrip got=%d err=%v", got, err)
		}
		if got, err := mustNew(t, "12.99").Int64(); err != nil || got != 12 {
			t.Fatalf("Int64 trunc got=%d err=%v", got, err)
		}

		assertString(t, FromUint64(0), "0")
		assertString(t, FromInt(7), "7")

		n := FromFloat64(1.25, 2)
		if n.GetPrecision() != 2 {
			t.Fatalf("FromFloat64 precision got=%d want=2", n.GetPrecision())
		}
		assertString(t, n, "1.25")
	})
}

func TestNumber_Sign_Clone_Abs_Neg(t *testing.T) {
	withDefaultPrecision(t, 8, func() {
		n := mustNew(t, "-1.5")
		if !n.IsNegative() || n.IsPositive() || n.Sign() != -1 {
			t.Fatalf("sign checks failed for %s", n.String())
		}
		if n.Abs().String() != "1.5" {
			t.Fatalf("Abs got %s", n.Abs().String())
		}
		if n.Neg().String() != "1.5" {
			t.Fatalf("Neg got %s", n.Neg().String())
		}

		z := Zero()
		if z.Sign() != 0 || z.IsNegative() || z.IsPositive() {
			t.Fatalf("zero sign checks failed")
		}
		if z.Neg().String() != "0" {
			t.Fatalf("Neg(0) got %s", z.Neg().String())
		}

		c := n.Clone()
		if c.String() != n.String() || c == n {
			t.Fatalf("Clone mismatch or same pointer")
		}
	})
}

func TestArithmetic_Add_Sub_Mul_Div(t *testing.T) {
	withDefaultPrecision(t, 8, func() {
		a := mustNew(t, "1.25")
		b := mustNew(t, "2.5")
		assertString(t, a.Add(b), "3.75")
		assertString(t, b.Sub(a), "1.25")
		assertString(t, a.Mul(b), "3.125")

		q, err := b.Div(a, 6)
		if err != nil {
			t.Fatalf("Div error: %v", err)
		}
		assertAlmost(t, q, 2.0, 1e-6)

		if _, err := a.Div(Zero()); err == nil {
			t.Fatalf("expected division by zero error")
		}

		m, err := mustNew(t, "-10").Mod(mustNew(t, "3"))
		if err != nil {
			t.Fatalf("Mod error: %v", err)
		}
		assertString(t, m, "-1")
	})
}

func TestArithmetic_QuoRem_PowInt_SqrtInt(t *testing.T) {
	withDefaultPrecision(t, 8, func() {
		quo, rem, err := mustNew(t, "17").QuoRem(mustNew(t, "5"))
		if err != nil {
			t.Fatalf("QuoRem error: %v", err)
		}
		assertString(t, quo, "3")
		assertString(t, rem, "2")

		assertString(t, mustNew(t, "2").PowInt(10), "1024")
		assertString(t, mustNew(t, "81").SqrtInt(), "9")
	})
}

func TestComparison(t *testing.T) {
	withDefaultPrecision(t, 8, func() {
		a := mustNew(t, "1.20")
		b := mustNew(t, "1.2")
		if !a.Equal(b) || a.NotEqual(b) {
			t.Fatalf("Equal/NotEqual mismatch")
		}
		if !a.LTE(b) || !a.GTE(b) || a.LT(b) || a.GT(b) {
			t.Fatalf("comparison mismatch for equal values")
		}

		if !mustNew(t, "2").Between(One(), FromInt(3)) {
			t.Fatalf("Between failed")
		}
		assertString(t, mustNew(t, "10").Clamp(One(), FromInt(3)), "3")
		if !mustNew(t, "4").IsInteger() || !mustNew(t, "4").IsEven() || mustNew(t, "4").IsOdd() {
			t.Fatalf("integer/even/odd checks failed")
		}
	})
}

func TestCodec_JSON(t *testing.T) {
	withDefaultPrecision(t, 8, func() {
		n := mustNew(t, "123.45")
		b, err := json.Marshal(n)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var out Number
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if out.String() != "123.45" {
			t.Fatalf("roundtrip got %q", out.String())
		}

		var out2 Number
		if err := json.Unmarshal([]byte("12.5"), &out2); err != nil {
			t.Fatalf("Unmarshal numeric error: %v", err)
		}
		if out2.String() != "12.5" {
			t.Fatalf("numeric unmarshal got %q", out2.String())
		}
	})
}

func TestConstants(t *testing.T) {
	withDefaultPrecision(t, 10, func() {
		pi := Pi(10)
		assertAlmost(t, pi, math.Pi, 1e-8)
		e := E(10)
		assertAlmost(t, e, math.E, 1e-8)
		phi := Phi(10)
		assertAlmost(t, phi, (1+math.Sqrt(5))/2, 1e-8)
		tau := Tau(10)
		assertAlmost(t, tau, 2*math.Pi, 1e-8)

		if PiCached() == nil || PiCached().String()[:2] != "3." {
			t.Fatalf("PiCached looks wrong")
		}
	})
}

func TestFormatting(t *testing.T) {
	withDefaultPrecision(t, 8, func() {
		n := mustNew(t, "-12345.6")
		opt := DefaultFormat()
		opt.ThousandsSep = ","
		opt.MinDecimals = 2
		if got := n.Format(opt); got != "-12,345.60" {
			t.Fatalf("Format got %q", got)
		}
		if got := mustNew(t, "1.23456").Fixed(2); got != "1.23" {
			t.Fatalf("Fixed got %q", got)
		}
		if got := mustNew(t, "1234").Scientific(); got != "1.234e+3" {
			t.Fatalf("Scientific got %q", got)
		}
		if got := mustNew(t, "1234").Engineering(); got != "1.234e+3" {
			t.Fatalf("Engineering got %q", got)
		}
		if got := mustNew(t, "0.125").Percent(1); got != "12.5%" {
			t.Fatalf("Percent got %q", got)
		}
		if got := mustNew(t, "1234.5").Currency("$", 2); got != "$1,234.50" {
			t.Fatalf("Currency got %q", got)
		}
		if got := mustNew(t, "2048").Bytes(); got != "2.00 KB" {
			t.Fatalf("Bytes got %q", got)
		}
		if got := mustNew(t, "1").GoString(); got == "" || got[0] != '&' {
			t.Fatalf("GoString got %q", got)
		}
	})
}

func TestMath_Round_Floor_Ceil_Trunc_Frac(t *testing.T) {
	withDefaultPrecision(t, 8, func() {
		n := mustNew(t, "1.235")
		assertString(t, n.Round(2), "1.24")

		assertString(t, mustNew(t, "1.1").Floor(), "1")
		assertString(t, mustNew(t, "-1.1").Floor(), "-2")
		assertString(t, mustNew(t, "1.1").Ceil(), "2")
		assertString(t, mustNew(t, "-1.1").Ceil(), "-1")
		assertString(t, mustNew(t, "-1.9").Trunc(), "-1")
		assertString(t, mustNew(t, "-1.90").Frac(), "-0.9")
	})
}

func TestMath_Sqrt_Pow_Hypot_Signum_Factorial_GCD_LCM(t *testing.T) {
	withDefaultPrecision(t, 10, func() {
		sq, err := mustNew(t, "2").SetPrecision(10).Sqrt()
		if err != nil {
			t.Fatalf("Sqrt error: %v", err)
		}
		assertAlmost(t, sq, math.Sqrt2, 1e-8)
		if _, err := mustNew(t, "-1").Sqrt(); err == nil {
			t.Fatalf("expected sqrt negative error")
		}

		p, err := mustNew(t, "9").SetPrecision(10).Pow(mustNew(t, "0.5").SetPrecision(10))
		if err != nil {
			t.Fatalf("Pow error: %v", err)
		}
		assertAlmost(t, p, 3.0, 1e-8)

		h := Hypot(mustNew(t, "3"), mustNew(t, "4"))
		assertAlmost(t, h.SetPrecision(10), 5.0, 1e-8)

		assertString(t, mustNew(t, "-10").Signum(), "-1")

		fact, err := Factorial(mustNew(t, "5"))
		if err != nil {
			t.Fatalf("Factorial error: %v", err)
		}
		assertString(t, fact, "120")
		if _, err := Factorial(mustNew(t, "2.5")); err == nil {
			t.Fatalf("expected factorial non-integer error")
		}

		assertString(t, GCD(mustNew(t, "12"), mustNew(t, "18")), "6")
		assertString(t, LCM(mustNew(t, "12"), mustNew(t, "18")), "36")
	})
}

func TestLog_Exp(t *testing.T) {
	withDefaultPrecision(t, 12, func() {
		two := mustNew(t, "2").SetPrecision(12)
		ln2, err := two.Ln()
		if err != nil {
			t.Fatalf("Ln error: %v", err)
		}
		assertAlmost(t, ln2, math.Ln2, 1e-9)

		exp1 := One().SetPrecision(12).Exp()
		assertAlmost(t, exp1, math.E, 1e-9)

		l10, err := mustNew(t, "1000").SetPrecision(12).Log10()
		if err != nil {
			t.Fatalf("Log10 error: %v", err)
		}
		assertAlmost(t, l10, 3.0, 1e-9)

		l2, err := mustNew(t, "8").SetPrecision(12).Log2()
		if err != nil {
			t.Fatalf("Log2 error: %v", err)
		}
		assertAlmost(t, l2, 3.0, 1e-9)

		if _, err := Zero().SetPrecision(12).Ln(); err == nil {
			t.Fatalf("expected ln(0) error")
		}
	})
}

func TestTrig(t *testing.T) {
	withDefaultPrecision(t, 12, func() {
		zero := Zero().SetPrecision(12)
		assertAlmost(t, zero.Sin(), 0.0, 1e-10)
		assertAlmost(t, zero.Cos(), 1.0, 1e-10)

		pi := Pi(12)
		halfPi, _ := pi.Div(Two(), 12)
		assertAlmost(t, halfPi.Sin(), 1.0, 1e-8)
		assertAlmost(t, pi.Cos(), -1.0, 1e-8)

		a := mustNew(t, "0.5").SetPrecision(12)
		asin, err := a.ASin()
		if err != nil {
			t.Fatalf("ASin error: %v", err)
		}
		assertAlmost(t, asin, math.Asin(0.5), 1e-8)

		acos, err := a.ACos()
		if err != nil {
			t.Fatalf("ACos error: %v", err)
		}
		assertAlmost(t, acos, math.Acos(0.5), 1e-8)

		assertAlmost(t, mustNew(t, "1").SetPrecision(12).ATan(), math.Pi/4, 1e-8)

		deg := mustNew(t, "180").SetPrecision(12)
		assertAlmost(t, deg.DegToRad(), math.Pi, 1e-8)
		assertAlmost(t, pi.RadToDeg(), 180.0, 1e-6)

		x := mustNew(t, "1.5").SetPrecision(12)
		assertAlmost(t, x.SinH(), math.Sinh(1.5), 1e-8)
		assertAlmost(t, x.CosH(), math.Cosh(1.5), 1e-8)
		assertAlmost(t, x.TanH(), math.Tanh(1.5), 1e-8)

		if _, err := mustNew(t, "2").SetPrecision(12).ASin(); err == nil {
			t.Fatalf("expected ASin out-of-range error")
		}
	})
}

func TestFromBytes_ToBytes(t *testing.T) {
	withDefaultPrecision(t, 0, func() {
		tests := []struct {
			name     string
			input    []byte
			expected string
		}{
			{"zero", []byte{0}, "0"},
			{"single byte 42", []byte{42}, "42"},
			{"two bytes 0x01, 0x00", []byte{0x01, 0x00}, "256"},
			{"two bytes 0xFF, 0xFF", []byte{0xFF, 0xFF}, "65535"},
			{"three bytes", []byte{0x01, 0x00, 0x00}, "65536"},
			{"address-like", []byte{0x00, 0x01, 0x2A, 0x07}, "76295"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				n := FromBytes(tt.input)
				if n == nil {
					t.Fatalf("FromBytes(%v) returned nil", tt.input)
				}
				if n.String() != tt.expected {
					t.Fatalf("FromBytes(%v).String() = %q, want %q", tt.input, n.String(), tt.expected)
				}
			})
		}
	})
}

