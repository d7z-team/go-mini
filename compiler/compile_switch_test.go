package compiler

import "testing"

func TestCompileSourceValidatesTypeSwitchSubjectAndCases(t *testing.T) {
	runCompileDiagnosticCases(t, "example/typeswitch", []compileDiagnosticCase{
		{
			name: "concrete subject",
			source: `package main
func Main() int64 {
	var value int64
	switch value.(type) { case int64: return 1 }
	return 0
}`,
			code: "hirgen.typeswitch.subject.interface",
		},
		{
			name: "case does not implement subject",
			source: `package main
type Reader interface { Read() int64 }
func Main(value Reader) int64 {
	switch value.(type) { case string: return 1 }
	return 0
}`,
			code: "hirgen.typeswitch.case.implements",
		},
		{
			name: "duplicate case",
			source: `package main
func Main(value any) int64 {
	switch value.(type) { case int64: return 1; case int64: return 2 }
	return 0
}`,
			code: "hirgen.typeswitch.case.duplicate",
		},
		{
			name: "duplicate nil",
			source: `package main
func Main(value any) int64 {
	switch value.(type) { case nil, nil: return 1 }
	return 0
}`,
			code: "parser.typeswitch.nil.duplicate",
		},
		{
			name: "guard assignment",
			source: `package main
func Main(value any) {
	var out any
	switch out = value.(type) { case int: _ = out }
}`,
			code: "parser.typeswitch.guard.assign",
		},
		{
			name: "guard multiple binding",
			source: `package main
func Main(value any) {
	switch a, b := value.(type) { case int: _, _ = a, b }
}`,
			code: "parser.typeswitch.guard.binding",
		},
		{
			name: "guard selector binding",
			source: `package main
type box struct { value any }
func Main(value any) {
	var target box
	switch target.value := value.(type) { case int: _ = target }
}`,
			code: "parser.typeswitch.guard.binding",
		},
		{
			name: "guard blank binding",
			source: `package main
func Main(value any) {
	switch _ := value.(type) { case int: }
}`,
			code: "parser.typeswitch.guard.blank",
		},
	})
}

func TestCompileSourceValidatesSwitchCases(t *testing.T) {
	runCompileDiagnosticCases(t, "example/switch", []compileDiagnosticCase{
		{
			name: "expressionless case must be bool",
			source: `package main
func Main() int64 {
	switch { case 1: return 1 }
	return 0
}`,
			code: "semantic.assign.type",
		},
		{
			name: "tag must be comparable",
			source: `package main
func Main() int64 {
	var value []int64
	switch value { default: return 1 }
	return 0
}`,
			code: "hirgen.switch.tag.comparable",
		},
		{
			name: "duplicate constant across clauses",
			source: `package main
func Main(value int64) int64 {
	switch value { case 1: return 1; case 1: return 2 }
	return 0
			}`,
			code: "hirgen.switch.case.duplicate",
		},
		{
			name: "duplicate constant in same clause",
			source: `package main
func Main(value int64) int64 {
	switch value { case 1, 1: return 1 }
	return 0
}`,
			code: "hirgen.switch.case.duplicate",
		},
		{
			name: "duplicate after tag type conversion",
			source: `package main
type Score int64
func Main(value Score) int64 {
	switch value { case 1: return 1; case Score(1): return 2 }
	return 0
}`,
			code: "hirgen.switch.case.duplicate",
		},
		{
			name: "duplicate named string constants",
			source: `package main
type Key string
const A Key = "go"
const B = "go"
func Main(value Key) int64 {
	switch value { case A: return 1; case B: return 2 }
	return 0
}`,
			code: "hirgen.switch.case.duplicate",
		},
		{
			name: "duplicate expressionless bool constants",
			source: `package main
func Main() int64 {
	switch { case true: return 1; case true: return 2 }
	return 0
}`,
			code: "hirgen.switch.case.duplicate",
		},
		{
			name: "duplicate default",
			source: `package main
func Main(value int64) int64 {
	switch value { default: return 1; default: return 2 }
	return 0
}`,
			code: "hirgen.switch.default.duplicate",
		},
	})
}
