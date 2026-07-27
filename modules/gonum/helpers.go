package gonum

import (
	"strconv"
	"strings"
)

// removeLeadingZeros removes leading zeros from a digit string.
// Keeps at least one digit ("0" if empty).
func removeLeadingZeros(s string) string {
	i := 0
	for i < len(s)-1 && s[i] == '0' {
		i++
	}
	return s[i:]
}

// removeTrailingZeros removes trailing zeros from a string.
func removeTrailingZeros(s string) string {
	i := len(s)
	for i > 0 && s[i-1] == '0' {
		i--
	}
	return s[:i]
}

// padLeft pads string s on the left with '0' to reach length.
func padLeft(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return strings.Repeat("0", length-len(s)) + s
}

// padRight pads string s on the right with '0' to reach length.
func padRight(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return s + strings.Repeat("0", length-len(s))
}

// imax returns the maximum of two ints.
func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// imin returns the minimum of two ints.
func imin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// iabs returns the absolute value of an int.
func iabs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// addStrings adds two non-negative integer strings.
func addStrings(a, b string) string {
	i, j := len(a)-1, len(b)-1
	carry := 0
	result := make([]byte, 0, imax(len(a), len(b))+1)

	for i >= 0 || j >= 0 || carry > 0 {
		sum := carry
		if i >= 0 {
			sum += int(a[i] - '0')
			i--
		}
		if j >= 0 {
			sum += int(b[j] - '0')
			j--
		}
		result = append(result, byte(sum%10+'0'))
		carry = sum / 10
	}

	// Reverse
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}

	return string(result)
}

// subStrings subtracts b from a, where a >= b (both non-negative integer strings).
func subStrings(a, b string) string {
	i, j := len(a)-1, len(b)-1
	borrow := 0
	result := make([]byte, len(a))
	pos := len(a) - 1

	for i >= 0 {
		diff := int(a[i]-'0') - borrow
		if j >= 0 {
			diff -= int(b[j] - '0')
			j--
		}
		if diff < 0 {
			diff += 10
			borrow = 1
		} else {
			borrow = 0
		}
		result[pos] = byte(diff + '0')
		pos--
		i--
	}

	return removeLeadingZeros(string(result))
}

// cmpStrings compares two non-negative integer strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func cmpStrings(a, b string) int {
	a = removeLeadingZeros(a)
	b = removeLeadingZeros(b)

	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	for i := 0; i < len(a); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// mulStrings multiplies two non-negative integer strings.
func mulStrings(a, b string) string {
	if a == "0" || b == "0" {
		return "0"
	}

	n, m := len(a), len(b)
	result := make([]int, n+m)

	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			mul := int(a[i]-'0') * int(b[j]-'0')
			p1, p2 := i+j, i+j+1
			sum := mul + result[p2]

			result[p2] = sum % 10
			result[p1] += sum / 10
		}
	}

	// Convert to string, skip leading zeros
	i := 0
	for i < len(result) && result[i] == 0 {
		i++
	}

	if i == len(result) {
		return "0"
	}

	sb := strings.Builder{}
	for ; i < len(result); i++ {
		sb.WriteByte(byte(result[i] + '0'))
	}

	return sb.String()
}

// divStrings divides a by b with the given decimal precision.
// Returns quotient (may contain ".") and remainder.
func divStrings(a, b string, precision int) (string, string) {
	if b == "0" {
		return "", ""
	}

	cmp := cmpStrings(a, b)
	if cmp < 0 && precision <= 0 {
		return "0", a
	}
	if cmp == 0 && precision <= 0 {
		return "1", "0"
	}

	intQuot, remainder := longDiv(a, b)
	intQuot = removeLeadingZeros(intQuot)

	if precision <= 0 || remainder == "0" {
		return intQuot, remainder
	}

	decPart, finalRem := decimalDiv(remainder, b, precision)
	return intQuot + "." + decPart, finalRem
}

// longDiv performs integer long division.
func longDiv(a, b string) (string, string) {
	if cmpStrings(a, b) < 0 {
		return "0", a
	}

	result := make([]byte, len(a))
	current := ""

	for i := 0; i < len(a); i++ {
		current += string(a[i])
		current = removeLeadingZeros(current)
		if current == "" {
			current = "0"
		}

		digit := 0
		for cmpStrings(current, b) >= 0 {
			current = subStrings(current, b)
			digit++
		}
		result[i] = byte(digit + '0')
	}

	return string(result), current
}

// decimalDiv continues division to produce decimal places.
func decimalDiv(remainder, divisor string, precision int) (string, string) {
	result := make([]byte, precision)
	current := remainder

	for i := 0; i < precision; i++ {
		current += "0"
		current = removeLeadingZeros(current)
		if current == "" {
			current = "0"
		}

		digit := 0
		for cmpStrings(current, divisor) >= 0 {
			current = subStrings(current, divisor)
			digit++
		}
		result[i] = byte(digit + '0')
	}

	return string(result), current
}

// divModStrings performs integer division with remainder.
func divModStrings(a, b string) (string, string) {
	return longDiv(a, b)
}

// pow10String returns 10^n as a string (integer).
func pow10String(n int) string {
	if n <= 0 {
		return "1"
	}
	return "1" + strings.Repeat("0", n)
}

// factorialString computes n! as a string.
func factorialString(n int) string {
	if n <= 1 {
		return "1"
	}
	result := "1"
	for i := 2; i <= n; i++ {
		result = mulStrings(result, strconv.Itoa(i))
	}
	return result
}

// shiftLeft multiplies a digit string by 10^n (adds n zeros).
func shiftLeft(s string, n int) string {
	if n <= 0 {
		return s
	}
	return s + strings.Repeat("0", n)
}

// shiftRight divides a digit string by 10^n (removes last n digits).
func shiftRight(s string, n int) string {
	if n <= 0 || n >= len(s) {
		return "0"
	}
	return s[:len(s)-n]
}
