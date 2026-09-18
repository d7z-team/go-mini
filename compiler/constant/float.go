package constant

import (
	"math"
	"strconv"
	"strings"
)

// RoundRationalFloat returns the exact binary value represented by an IEEE
// conversion. Subsequent typed constant operations consume this rounded value.
func RoundRationalFloat(value Rational, bits int) (Rational, bool) {
	rounded, ok := RationalFloat(value, bits)
	if !ok {
		return Rational{}, false
	}
	encoded := math.Float64bits(rounded)
	exponent := int((encoded >> 52) & 0x7ff)
	mantissa := encoded & ((uint64(1) << 52) - 1)
	if exponent != 0 {
		mantissa |= uint64(1) << 52
	} else {
		exponent = 1
	}
	exponent -= 1023 + 52
	if mantissa == 0 {
		return Rational{Numerator: "0", Denominator: "1"}, true
	}
	// The denominator is a power of two, so reduce common factors before
	// materializing decimal strings. The remaining pair is already coprime.
	for exponent < 0 && mantissa&1 == 0 {
		mantissa >>= 1
		exponent++
	}
	numerator, denominator := strconv.FormatUint(mantissa, 10), "1"
	if exponent >= 0 {
		numerator, _ = MultiplySignedDecimal(numerator, Pow2UnsignedDecimal(exponent))
	} else {
		denominator = Pow2UnsignedDecimal(-exponent)
	}
	if encoded>>63 != 0 && mantissa != 0 {
		numerator = "-" + numerator
	}
	return Rational{Numerator: numerator, Denominator: denominator}, true
}

// RationalFloat rounds an exact rational to a finite IEEE value using ties to even.
func RationalFloat(value Rational, bits int) (float64, bool) {
	if !value.Valid() {
		return 0, false
	}
	negative := strings.HasPrefix(value.Numerator, "-")
	numerator := strings.TrimPrefix(value.Numerator, "-")
	if numerator == "0" {
		return signedFloatZero(negative), true
	}
	precision, minimumExponent, maximumExponent := 53, -1022, 1023
	if bits == 32 {
		precision, minimumExponent, maximumExponent = 24, -126, 127
	} else if bits != 64 {
		return 0, false
	}
	decimalDifference := len(numerator) - len(value.Denominator)
	if bits == 64 && decimalDifference > 310 || bits == 32 && decimalDifference > 40 {
		return 0, false
	}
	if bits == 64 && decimalDifference < -400 || bits == 32 && decimalDifference < -60 {
		return signedFloatZero(negative), true
	}
	exponent := estimateBinaryExponent(numerator, value.Denominator)
	if exponent > maximumExponent {
		return 0, false
	}
	var significand string
	if exponent < minimumExponent {
		shift := precision - 1 - minimumExponent
		significand = roundScaledRational(numerator, value.Denominator, shift)
		if significand == "" {
			return 0, false
		}
		minimumNormalText := Pow2UnsignedDecimal(precision - 1)
		if CompareUnsignedDecimal(significand, minimumNormalText) >= 0 {
			exponent = minimumExponent
		}
	} else {
		shift := precision - 1 - exponent
		significand = roundScaledRational(numerator, value.Denominator, shift)
		if significand == "" {
			return 0, false
		}
		limit := Pow2UnsignedDecimal(precision)
		if CompareUnsignedDecimal(significand, limit) >= 0 {
			significand = HalveUnsignedDecimal(significand)
			exponent++
			if exponent > maximumExponent {
				return 0, false
			}
		}
	}
	parsed, err := strconv.ParseUint(significand, 10, 64)
	if err != nil {
		return 0, false
	}
	minimumNormal := uint64(1) << uint(precision-1)
	binaryShift := exponent - (precision - 1)
	if exponent < minimumExponent && parsed < minimumNormal {
		binaryShift = minimumExponent - (precision - 1)
	}
	out := float64(parsed)
	if binaryShift >= 0 {
		for i := 0; i < binaryShift; i++ {
			out *= 2
		}
	} else {
		for i := 0; i > binaryShift; i-- {
			out /= 2
		}
	}
	if negative {
		out = -out
	}
	return out, true
}

func signedFloatZero(negative bool) float64 {
	zero := float64(0)
	if negative {
		return -zero
	}
	return zero
}

func estimateBinaryExponent(numerator, denominator string) int {
	// 217706/65536 approximates log2(10). Exact comparisons below correct
	// the few remaining bits without rescanning the entire exponent range.
	exponent := int(int64(len(numerator)-len(denominator)) * 217706 / 65536)
	for compareRatioWithPowerOfTwo(numerator, denominator, exponent) < 0 {
		exponent--
	}
	for compareRatioWithPowerOfTwo(numerator, denominator, exponent+1) >= 0 {
		exponent++
	}
	return exponent
}

func compareRatioWithPowerOfTwo(numerator, denominator string, exponent int) int {
	if exponent >= 0 {
		scaled, _ := MultiplySignedDecimal(denominator, Pow2UnsignedDecimal(exponent))
		return CompareUnsignedDecimal(numerator, scaled)
	}
	scaled, _ := MultiplySignedDecimal(numerator, Pow2UnsignedDecimal(-exponent))
	return CompareUnsignedDecimal(scaled, denominator)
}

func roundScaledRational(numerator, denominator string, binaryShift int) string {
	if binaryShift >= 0 {
		numerator, _ = MultiplySignedDecimal(numerator, Pow2UnsignedDecimal(binaryShift))
	} else {
		denominator, _ = MultiplySignedDecimal(denominator, Pow2UnsignedDecimal(-binaryShift))
	}
	quotient, remainder, ok := DivideUnsignedDecimal(numerator, denominator)
	if !ok {
		return ""
	}
	twiceRemainder := AddUnsignedDecimal(remainder, remainder)
	comparison := CompareUnsignedDecimal(twiceRemainder, denominator)
	lastDigitOdd := quotient[len(quotient)-1]%2 != 0
	if comparison > 0 || comparison == 0 && lastDigitOdd {
		quotient = AddUnsignedDecimal(quotient, "1")
	}
	return quotient
}
