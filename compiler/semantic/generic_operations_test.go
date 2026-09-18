package semantic

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/parser"
	"github.com/d7z-team/mini-go/compiler/types"
)

func TestAnalyzeGenericOperationsAndCallKinds(t *testing.T) {
	parsed := parser.ParseSource("example/generic", "generic.mgo", `package generic

type Number interface { ~int | ~int64 }
func Convert[T Number](v int, a, b T) T { return T(v) + max(a, b) }
func InvalidAny[T any](v T) T { return v + v }
type Mixed interface { ~int | ~string }
func InvalidRemainder[T Mixed](v T) T { return v % v }
func InvalidMethod[T any](v T) { v.Missing() }
func InvalidRange[T interface { ~int | ~int8 }](v T) { for range v {} }
func InvalidStatement() { min(1, 2) }
`)
	if len(parsed.Diagnostics) != 0 {
		t.Fatalf("parse source: %#v", parsed.Diagnostics)
	}
	info := Check(parsed.Program).Info
	codes := map[string]bool{}
	for _, diagnostic := range info.Diagnostics {
		codes[string(diagnostic.Code)] = true
	}
	for _, code := range []string{
		"semantic.generic.binary_constraint",
		"semantic.generic.selector",
		"semantic.generic.range_constraint",
		"semantic.statement.builtin_value",
	} {
		if !codes[code] {
			t.Fatalf("missing diagnostic %q: %#v", code, info.Diagnostics)
		}
	}

	var conversion, builtin bool
	ast.WalkExpressions(&parsed.Program, func(expr *ast.Expression) {
		call, ok := info.Calls[expr.NodeID]
		if !ok {
			return
		}
		conversion = conversion || call.Kind == CallConversion && call.Target.Kind == types.TypeParameter
		builtin = builtin || call.Kind == CallBuiltin
	})
	if !conversion || !builtin {
		t.Fatalf("missing call facts: conversion=%v builtin=%v calls=%#v", conversion, builtin, info.Calls)
	}
}

func TestAnalyzeAcceptsParameterizedGenericOperations(t *testing.T) {
	parsed := parser.ParseSource("example/genericops", "genericops.mgo", `package genericops

type Slice[E any] interface { ~[]E }
type Map[K comparable, V any] interface { ~map[K]V }
type Chan[E any] interface { ~chan E }

func Index[S Slice[E], E any](v S) E { return v[0] }
func SliceValue[S Slice[E], E any](v S) S { return v[:] }
func RangeOver[S Slice[E], E any](v S) { for range v {} }
func Receive[C Chan[E], E any](v C) E { return <-v }
func Send[C Chan[E], E any](v C, e E) { v <- e }
func Length[T interface { ~string | ~[]byte }](v T) int { return len(v) }
func Capacity[T interface { ~[]byte | ~chan byte }](v T) int { return cap(v) }
func AppendOne[S Slice[E], E any](v S, e E) S { return append(v, e) }
func ClearValue[T interface { ~[]int | ~map[int]int }](v T) { clear(v) }
func CloseChan[C Chan[E], E any](v C) { close(v) }
func DeleteKey[M Map[K, V], K comparable, V any](v M, k K) { delete(v, k) }
func CopyString[S ~[]byte](v S, text string) int { return copy(v, text) }
func MakeSlice[S ~[]int](n int) S { return make(S, n) }
func NilSlice[S ~[]int](v S) bool { return v == nil }
func NilMap[M ~map[string]int](v M) bool { return nil != v }
func NilPointer[P ~*int](v P) bool { return v == nil }
func NilFunction[F ~func()](v F) bool { return v != nil }
func NilChannel[C ~chan int](v C) bool { return nil == v }
func Pointer[T ~*int](v T) int { return *v }
func LenPointer[T ~*[3]int](v T) int { return len(v) }
func Convert[T interface { ~int | ~int64 }](v int) T { return T(v) }
func ConvertToAny[T any](v T) any { return interface{}(v) }
`)
	if len(parsed.Diagnostics) != 0 {
		t.Fatalf("parse source: %#v", parsed.Diagnostics)
	}
	if diagnostics := Check(parsed.Program).Info.Diagnostics; len(diagnostics) != 0 {
		t.Fatalf("generic operation diagnostics: %#v", diagnostics)
	}
}

func TestAnalyzeRejectsInvalidGenericOperationConstraints(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{"index", `func F[T any](v T) { _ = v[0] }`, "semantic.generic.index_constraint"},
		{"slice", `func F[T any](v T) { _ = v[:] }`, "semantic.generic.slice_constraint"},
		{"range", `func F[T any](v T) { for range v {} }`, "semantic.generic.range_constraint"},
		{"receive", `func F[T any](v T) { _ = <-v }`, "semantic.generic.receive_constraint"},
		{"send", `func F[T any](v T) { v <- 1 }`, "semantic.generic.send_constraint"},
		{"dereference", `func F[T any](v T) { _ = *v }`, "semantic.generic.deref_constraint"},
		{"len", `func F[T any](v T) int { return len(v) }`, "semantic.generic.builtin_constraint"},
		{"append", `func F[T any](v T) { _ = append(v, 1) }`, "semantic.generic.builtin_constraint"},
		{"clear", `func F[T any](v T) { clear(v) }`, "semantic.generic.builtin_constraint"},
		{"make", `func F[T any](n int) T { return make(T, n) }`, "semantic.generic.builtin_constraint"},
		{"complex", `func F[T ~float32](a, b T) { _ = complex(a, b) }`, "semantic.generic.builtin_constraint"},
		{"conversion any", `func F[T any](v int) T { return T(v) }`, "semantic.generic.conversion"},
		{"conversion bool", `func F[T ~bool](v int) T { return T(v) }`, "semantic.generic.conversion"},
		{"nil comparison", `func F[T interface { ~[]int | ~int }](v T) bool { return v == nil }`, "semantic.generic.binary_constraint"},
		{"parameterized partial type set", `type Mixed[E any] interface { ~[]E | ~map[int]E }
func F[T Mixed[int]](v T) { _ = v[:] }`, "semantic.generic.slice_constraint"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := parser.ParseSource("example/invalid", "invalid.mgo", "package invalid\n"+test.body)
			if len(parsed.Diagnostics) != 0 {
				t.Fatalf("parse source: %#v", parsed.Diagnostics)
			}
			diagnostics := Check(parsed.Program).Info.Diagnostics
			for _, diagnostic := range diagnostics {
				if string(diagnostic.Code) == test.code {
					return
				}
			}
			t.Fatalf("missing diagnostic %q: %#v", test.code, diagnostics)
		})
	}
}
