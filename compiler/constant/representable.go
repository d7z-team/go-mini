package constant

import "strings"

// IntegerRepresentable reports whether an exact rational fits an integer target.
func IntegerRepresentable(value Rational, bits int, signed bool) bool {
	integer, ok := value.Integer()
	if !ok || bits < 1 {
		return false
	}
	if signed {
		lower, upper := SignedIntegerBounds(bits)
		return CompareSignedDecimal(integer, lower) >= 0 && CompareSignedDecimal(integer, upper) <= 0
	}
	upper := UnsignedIntegerMax(bits)
	return !strings.HasPrefix(integer, "-") && CompareUnsignedDecimal(integer, upper) <= 0
}

// FloatRepresentable checks finite IEEE round-to-nearest representation. The
// midpoint above the maximum finite number rounds to infinity and is excluded.
func FloatRepresentable(value Rational, bits int) bool {
	if !value.Valid() || bits != 32 && bits != 64 {
		return false
	}
	exponent, precision := 1024, 53
	if bits == 32 {
		exponent, precision = 128, 24
	}
	cutoff, _ := SubtractUnsignedDecimal(Pow2UnsignedDecimal(exponent), Pow2UnsignedDecimal(exponent-precision-1))
	limit, _ := NewRational(cutoff, "1")
	value.Numerator = strings.TrimPrefix(value.Numerator, "-")
	return CompareRational(value, limit) < 0
}

func SignedIntegerBounds(bits int) (string, string) {
	switch bits {
	case 8:
		return "-128", "127"
	case 16:
		return "-32768", "32767"
	case 32:
		return "-2147483648", "2147483647"
	case 64:
		return "-9223372036854775808", "9223372036854775807"
	}
	if bits <= 0 {
		return "0", "0"
	}
	maxBase := Pow2UnsignedDecimal(bits - 1)
	upper, _ := SubtractUnsignedDecimal(maxBase, "1")
	return "-" + maxBase, upper
}

func UnsignedIntegerMax(bits int) string {
	switch bits {
	case 8:
		return "255"
	case 16:
		return "65535"
	case 32:
		return "4294967295"
	case 64:
		return "18446744073709551615"
	}
	if bits <= 0 {
		return "0"
	}
	upper, _ := SubtractUnsignedDecimal(Pow2UnsignedDecimal(bits), "1")
	return upper
}
