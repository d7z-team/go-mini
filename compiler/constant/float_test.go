package constant

import (
	"math"
	"math/big"
	"strings"
	"testing"
)

func TestRoundRationalFloatPreservesCanonicalBinaryValue(t *testing.T) {
	patterns := []uint64{0, 1, 2, 0xfffffffffffff, 0x10000000000000, 0x3ff0000000000000, 0x7fefffffffffffff}
	state := uint64(1)
	for range 128 {
		state = state*6364136223846793005 + 1442695040888963407
		patterns = append(patterns, state&0x7fefffffffffffff)
	}
	for _, pattern := range patterns {
		for _, negative := range []bool{false, true} {
			value := math.Float64frombits(pattern)
			if negative {
				value = -value
			}
			want := new(big.Rat).SetFloat64(value)
			input := Rational{Numerator: want.Num().String(), Denominator: want.Denom().String()}
			got, ok := RoundRationalFloat(input, 64)
			if !ok || got != input {
				t.Fatalf("bits %x negative %v: got %+v (%v), want %+v", pattern, negative, got, ok, input)
			}
		}
	}
}

func BenchmarkRoundRationalFloatSubnormal(b *testing.B) {
	value, ok := ParseRationalLiteral("1e-308")
	if !ok {
		b.Fatal("invalid benchmark constant")
	}
	for b.Loop() {
		_, _ = RoundRationalFloat(value, 64)
	}
}

func TestBinaryExponentMatchesExactRationalMagnitude(t *testing.T) {
	for power := 0; power <= 1200; power += 37 {
		large := "7" + strings.Repeat("0", power)
		for _, pair := range [][2]string{{large, "3"}, {"3", large}, {large, large}} {
			rat, ok := new(big.Rat).SetString(pair[0] + "/" + pair[1])
			if !ok {
				t.Fatal("invalid test rational")
			}
			value := new(big.Float).SetPrec(uint((power+1)*4 + 64)).SetRat(rat)
			want := value.MantExp(nil) - 1
			if got := estimateBinaryExponent(pair[0], pair[1]); got != want {
				t.Fatalf("power %d: exponent %d, want %d", power, got, want)
			}
		}
	}
}

func BenchmarkRationalFloatLargeExponent(b *testing.B) {
	value, ok := ParseRationalLiteral("1e308")
	if !ok {
		b.Fatal("invalid benchmark constant")
	}
	for b.Loop() {
		_, _ = RationalFloat(value, 64)
	}
}
