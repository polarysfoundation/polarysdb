package gonum

// Pi returns π to the specified precision.
// Uses Machin's formula: π/4 = 4*arctan(1/5) - arctan(1/239)
func Pi(precision int) *Number {
    if precision <= 0 {
        precision = DefaultPrecision
    }

    workPrecision := precision + 25

    // arctan(1/5)
    oneFifth, _ := One().Div(FromInt64(5), workPrecision)
    atanOneFifth := arctanSeries(oneFifth, workPrecision)

    // arctan(1/239)
    one239, _ := One().Div(FromInt64(239), workPrecision)
    atanOne239 := arctanSeries(one239, workPrecision)

    // π/4 = 4*arctan(1/5) - arctan(1/239)
    four := Four()
    piOver4 := atanOneFifth.Mul(four).Sub(atanOne239)

    // π = 4 * (π/4)
    pi := piOver4.Mul(four)

    return pi.Round(precision).SetPrecision(precision)
}

// arctanSeries computes arctan(x) using Taylor series for |x| < 1.
func arctanSeries(x *Number, precision int) *Number {
    // arctan(x) = x - x³/3 + x⁵/5 - x⁷/7 + ...
    term := x.Clone()
    sum := x.Clone()
    xSquared := x.Mul(x)

    maxTerms := precision*3 + 200
    if maxTerms < 400 {
        maxTerms = 400
    }

    for i := 1; i < maxTerms; i++ {
        // term_{i} = term_{i-1} * x^2 * (2i-1)/(2i+1)
        numFactor := FromInt64(2*int64(i) - 1)
        denFactor := FromInt64(2*int64(i) + 1)

        term = term.Mul(xSquared).Mul(numFactor)
        term, _ = term.Div(denFactor, precision)
        term.negative = !term.negative // alternating signs

        if term.IsZero() {
            break
        }

        sum = sum.Add(term)
    }

    return sum
}

// Four returns a Number equal to 4.
func Four() *Number {
    return &Number{digits: "4", scale: 0, precision: DefaultPrecision}
}

// E returns Euler's number e to the specified precision.
func E(precision int) *Number {
    if precision <= 0 {
        precision = DefaultPrecision
    }

    return One().SetPrecision(precision + 10).Exp().Round(precision).SetPrecision(precision)
}

// Phi returns the golden ratio (1 + √5) / 2 to the specified precision.
func Phi(precision int) *Number {
    if precision <= 0 {
        precision = DefaultPrecision
    }

    five := FromInt64(5).SetPrecision(precision + 10)
    sqrtFive, _ := five.Sqrt()
    one := One()
    two := Two()
    result, _ := one.Add(sqrtFive).Div(two, precision)

    return result.SetPrecision(precision)
}

// Tau returns τ = 2π to the specified precision.
func Tau(precision int) *Number {
    return Pi(precision).Mul(Two())
}

// PiCached returns π using a pre-computed string (fast, 1000 digits).
func PiCached() *Number {
    piStr := "3.1415926535897932384626433832795028841971693993751058209749445923078164062862089986280348253421170679821480865132823066470938446095505822317253594081284811174502841027019385211055596446229489549303819644288109756659334461284756482337867831652712019091456485669234603486104543266482133936072602491412737245870066063155881748815209209628292540917153643678925903600113305305488204665213841469519415116094330572703657595919530921861173819326117931051185480744623799627495673518857527248912279381830119491298336733624406566430860213949463952247371907021798609437027705392171762931767523846748184676694051320005681271452635608277857713427577896091736371787214684409012249534301465495853710507922796892589235420199561121290219608640344181598136297747713099605187072113499999983729780499510597317328160963185950244594553469083026425223082533446850352619311881710100031378387528865875332083814206171776691473035982534904287554687311595628638823537875937519577818577805321712268066130019278766111959092164201989"
    n := New(piStr)
    return n
}
