package compiler

import (
	"strings"
	"testing"
)

func TestCompileLowersOperatorMethodsToCalls(t *testing.T) {
	result, err := compileTestSource("example/operators", "operators.mgo", `package operators

type Value struct { Number int; Data []int }

func (v Value) OpAdd(other Value) Value { return Value{Number: v.Number + other.Number} }
func (v *Value) OpSub(other Value) Value { return Value{Number: v.Number - other.Number} }
func (v Value) OpEq(other Value) bool { return v.Number == other.Number }

func Apply(left Value, right Value) bool {
	left += right
	left -= right
	return left == right
}
`)
	if err != nil || !result.OK() {
		t.Fatalf("compile: %v %#v", err, result.Diagnostics)
	}
	disassembly, err := result.Disassemble()
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"method.Value.OpAdd", "method.Ptr<Value>.OpSub", "method.Value.OpEq"} {
		if !strings.Contains(disassembly, method) {
			t.Fatalf("missing operator call %q:\n%s", method, disassembly)
		}
	}
}

func TestCompileConvertsUntypedShiftExpressionsInBitwiseContext(t *testing.T) {
	result, err := compileTestSource("example/operators", "operators.mgo", `package operators

const wordBits = 32

func Seen(words []uint32, index uint) bool {
	return words[index/wordBits]&(1<<(index&(wordBits-1))) != 0
}

func Special(value byte, table [16]byte) bool {
	return table[value%16]&(1<<(value/16)) != 0
}
`)
	if err != nil || !result.OK() {
		t.Fatalf("compile: %v %#v", err, result.Diagnostics)
	}
}

func TestCompileConvertsUntypedNamedConstantInComparison(t *testing.T) {
	result, err := compileTestSource("example/operators", "operators.mgo", `package operators

const Max = '\u00ff'

func Latin(value rune) bool { return uint32(value) <= Max }
`)
	if err != nil || !result.OK() {
		t.Fatalf("compile: %v %#v", err, result.Diagnostics)
	}
}

func TestCompileResolvesUntypedConstantsBeforeCrossFileFunctionBodies(t *testing.T) {
	result, err := compileTestPackage(SourcePackage{ModulePath: "example/operators", Files: []SourceFile{
		{Path: "a_function.mgo", Text: `package operators
func Latin(value rune) bool { return uint32(value) <= Max }
`},
		{Path: "z_constant.mgo", Text: `package operators
const Max = '\u00ff'
`},
	}})
	if err != nil || !result.OK() {
		t.Fatalf("compile: %v %#v", err, result.Diagnostics)
	}
}

func TestCompileResolvesForwardUntypedConstantInGroup(t *testing.T) {
	result, err := compileTestSource("example/operators", "operators.mgo", `package operators

const (
	Max = 128 << 20 / WordSize
	WordSize = 5 * 8
)

func Fits(value int64) bool { return value < Max/value }
`)
	if err != nil || !result.OK() {
		t.Fatalf("compile: %v %#v", err, result.Diagnostics)
	}
}
