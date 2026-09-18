package constant

import (
	"math/big"
	"strings"
	"testing"
)

func TestIntegerLiteralParsingPreservesBasesSignsAndArbitraryPrecision(t *testing.T) {
	for _, input := range []string{
		"0", "-0", "+0", "000", "-000", "42", "-42", "+42", "0_7_7",
		"0xff", "-0XFF", "0b101010", "0o52", "077", "18446744073709551615",
		"18446744073709551616", "0x10000000000000000", "-0xffffffffffffffffffffffff",
		strings.Repeat("9", 1024),
	} {
		want, ok := new(big.Int).SetString(input, 0)
		if !ok {
			t.Fatalf("invalid test literal %q", input)
		}
		got, ok := ParseIntegerLiteral(input)
		if !ok || got.Text != want.String() || got.Type != "Int" || !got.Untyped {
			t.Fatalf("ParseIntegerLiteral(%q) = %#v, %v; want %s", input, got, ok, want)
		}
	}
	for _, input := range []string{"", "+", "-", "0x", "0b2", "08", "1e3", "1.2", "+ 42", "0x+1", "0x-1", "12 3", "４２", "1\x002"} {
		if got, ok := ParseIntegerLiteral(input); ok {
			t.Fatalf("ParseIntegerLiteral(%q) = %#v; want invalid input", input, got)
		}
	}
}

func TestValueIntegerArithmetic(t *testing.T) {
	left, ok := ParseIntegerLiteral("1_000_000_000_000_000_000_000")
	if !ok {
		t.Fatal("parse left integer")
	}
	right, _ := ParseIntegerLiteral("3")
	product, ok := Binary("*", left, right)
	if !ok || product.Text != "3000000000000000000000" {
		t.Fatalf("product = %#v, %v", product, ok)
	}
	shift, _ := ParseIntegerLiteral("10")
	shifted, ok := Binary("<<", right, shift)
	if !ok || shifted.Text != "3072" {
		t.Fatalf("shift = %#v, %v", shifted, ok)
	}
}

func TestValueIntegerArithmeticPreservesNegativeRightShift(t *testing.T) {
	left, _ := ParseIntegerLiteral("-3")
	right, _ := ParseIntegerLiteral("1")
	got, ok := Binary(">>", left, right)
	if !ok || got.Text != "-2" {
		t.Fatalf("-3 >> 1 = %#v, %v", got, ok)
	}
}
