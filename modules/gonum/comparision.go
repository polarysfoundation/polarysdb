package gonum

// Cmp compares n with other.
// Returns -1 if n < other, 0 if n == other, 1 if n > other.
func (n *Number) Cmp(other *Number) int {
    // Handle signs
    if n.negative && !other.negative {
        if n.IsZero() && other.IsZero() {
            return 0
        }
        return -1
    }
    if !n.negative && other.negative {
        if n.IsZero() && other.IsZero() {
            return 0
        }
        return 1
    }

    // Same sign
    cmp := cmpMagnitudes(n, other)
    if n.negative {
        return -cmp // reverse for negative
    }
    return cmp
}

// Equal returns true if n == other.
func (n *Number) Equal(other *Number) bool {
    return n.Cmp(other) == 0
}

// NotEqual returns true if n != other.
func (n *Number) NotEqual(other *Number) bool {
    return n.Cmp(other) != 0
}

// LT returns true if n < other.
func (n *Number) LT(other *Number) bool {
    return n.Cmp(other) < 0
}

// LTE returns true if n <= other.
func (n *Number) LTE(other *Number) bool {
    return n.Cmp(other) <= 0
}

// GT returns true if n > other.
func (n *Number) GT(other *Number) bool {
    return n.Cmp(other) > 0
}

// GTE returns true if n >= other.
func (n *Number) GTE(other *Number) bool {
    return n.Cmp(other) >= 0
}

// Max returns the larger of n and other.
func (n *Number) Max(other *Number) *Number {
    if n.GTE(other) {
        return n.Clone()
    }
    return other.Clone()
}

// Min returns the smaller of n and other.
func (n *Number) Min(other *Number) *Number {
    if n.LTE(other) {
        return n.Clone()
    }
    return other.Clone()
}

// IsInteger returns true if the number has no decimal part.
func (n *Number) IsInteger() bool {
    return n.scale == 0
}

// IsEven returns true if the integer value is even.
func (n *Number) IsEven() bool {
    if n.scale > 0 {
        return false
    }
    if len(n.digits) == 0 {
        return true
    }
    lastDigit := n.digits[len(n.digits)-1]
    return lastDigit == '0' || lastDigit == '2' || lastDigit == '4' || lastDigit == '6' || lastDigit == '8'
}

// IsOdd returns true if the integer value is odd.
func (n *Number) IsOdd() bool {
    if n.scale > 0 {
        return false
    }
    return !n.IsEven()
}

// Between returns true if min <= n <= max (inclusive).
func (n *Number) Between(min, max *Number) bool {
    return n.GTE(min) && n.LTE(max)
}

// Clamp returns n clamped to the range [min, max].
func (n *Number) Clamp(min, max *Number) *Number {
    if n.LT(min) {
        return min.Clone()
    }
    if n.GT(max) {
        return max.Clone()
    }
    return n.Clone()
}