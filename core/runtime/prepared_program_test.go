package runtime

import (
	"context"
	"strings"
	"testing"
)

func ptrString(v string) *string { return &v }
func ptrInt64(v int64) *int64    { return &v }

func TestNewExecutorFromPreparedRequiresPreparedProgram(t *testing.T) {
	_, err := NewExecutorFromPrepared(nil)
	if err == nil {
		t.Fatal("expected missing prepared program error")
	}
	if !strings.Contains(err.Error(), "missing prepared program") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewExecutorFromPreparedRejectsOrphanScopeExit(t *testing.T) {
	prepared := &PreparedProgram{
		Globals:   map[string]*PreparedGlobal{},
		Functions: map[string]*PreparedFunction{},
		MainTasks: []Task{{Op: OpScopeExit}},
	}
	_, err := NewExecutorFromPrepared(prepared)
	if err == nil {
		t.Fatal("expected invalid prepared program error")
	}
	if !strings.Contains(err.Error(), "exits without matching scope enter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewExecutorFromPreparedRejectsMissingConstantType(t *testing.T) {
	prepared := &PreparedProgram{
		Constants: map[string]FFIConstValue{"Answer": ConstInt64(42)},
	}
	if _, err := NewExecutorFromPrepared(prepared); err != nil {
		t.Fatalf("expected typed constant payload to provide its own type: %v", err)
	}
}

func TestNewExecutorFromPreparedRejectsUnsupportedConstantType(t *testing.T) {
	prepared := &PreparedProgram{
		Constants: map[string]FFIConstValue{
			"Data": {Type: SpecAny, String: ptrString("x")},
		},
	}
	_, err := NewExecutorFromPrepared(prepared)
	if err == nil {
		t.Fatal("expected unsupported constant type error")
	}
	if !strings.Contains(err.Error(), "constant Data invalid: unsupported ffi const type Any") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFFIConstValuePreservesByteAndRuneTypes(t *testing.T) {
	byteConst := ConstByte(65)
	if err := byteConst.Validate(); err != nil {
		t.Fatalf("byte const validate failed: %v", err)
	}
	byteValue := byteConst.ToVar()
	if byteValue == nil || byteValue.I64 != 65 || byteValue.RuntimeType().Raw != SpecByte {
		t.Fatalf("byte const runtime value = %#v", byteValue)
	}
	if byteConst.DisplayString() != "65" || byteConst.Hash() == ConstInt64(65).Hash() {
		t.Fatalf("byte const display/hash did not preserve subtype")
	}

	runeConst := ConstRune('你')
	if err := runeConst.Validate(); err != nil {
		t.Fatalf("rune const validate failed: %v", err)
	}
	runeValue := runeConst.ToVar()
	if runeValue == nil || runeValue.I64 != 20320 || runeValue.RuntimeType().Raw != SpecRune {
		t.Fatalf("rune const runtime value = %#v", runeValue)
	}
	if runeConst.DisplayString() != "20320" || runeConst.Hash() == ConstInt64(20320).Hash() {
		t.Fatalf("rune const display/hash did not preserve subtype")
	}

	sentinelRuneConst := ConstRune(-1)
	if err := sentinelRuneConst.Validate(); err != nil {
		t.Fatalf("negative rune const validate failed: %v", err)
	}
	sentinelRuneValue := sentinelRuneConst.ToVar()
	if sentinelRuneValue == nil || sentinelRuneValue.I64 != -1 || sentinelRuneValue.RuntimeType().Raw != SpecRune {
		t.Fatalf("negative rune const runtime value = %#v", sentinelRuneValue)
	}

	invalid := FFIConstValue{Type: SpecByte, Int64: ptrInt64(256)}
	if err := invalid.Validate(); err == nil || !strings.Contains(err.Error(), "overflows Byte") {
		t.Fatalf("expected invalid byte constant, got %v", err)
	}
}

func TestNewExecutorFromPreparedRejectsInvalidMethodReceiver(t *testing.T) {
	prepared := &PreparedProgram{
		Functions: map[string]*PreparedFunction{
			"Box.Call": {
				Name:        "Box.Call",
				Receiver:    TypeSpec("Box"),
				FunctionSig: MustParseRuntimeFuncSig("function(Ptr<Other>) Void"),
			},
		},
	}
	_, err := NewExecutorFromPrepared(prepared)
	if err == nil {
		t.Fatal("expected invalid receiver error")
	}
	if !strings.Contains(err.Error(), "receiver first parameter must be Box or Ptr<Box>, got Ptr<Other>") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewExecutorFromPreparedRejectsHostRefMethodReceiverParam(t *testing.T) {
	prepared := &PreparedProgram{
		Functions: map[string]*PreparedFunction{
			"Box.Call": {
				Name:        "Box.Call",
				Receiver:    TypeSpec("Box"),
				FunctionSig: MustParseRuntimeFuncSig("function(HostRef<Box>) Void"),
			},
		},
	}
	_, err := NewExecutorFromPrepared(prepared)
	if err == nil {
		t.Fatal("expected host reference receiver parameter error")
	}
	if !strings.Contains(err.Error(), "receiver first parameter must be Box or Ptr<Box>, got HostRef<Box>") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewExecutorFromPreparedAcceptsValueAndPointerMethodReceivers(t *testing.T) {
	prepared := &PreparedProgram{
		Functions: map[string]*PreparedFunction{
			"ValueBox.Call": {
				Name:        "ValueBox.Call",
				Receiver:    TypeSpec("ValueBox"),
				FunctionSig: MustParseRuntimeFuncSig("function(ValueBox) Void"),
			},
			"PointerBox.Call": {
				Name:        "PointerBox.Call",
				Receiver:    TypeSpec("PointerBox"),
				FunctionSig: MustParseRuntimeFuncSig("function(Ptr<PointerBox>) Void"),
			},
		},
	}
	if _, err := NewExecutorFromPrepared(prepared); err != nil {
		t.Fatalf("expected method receiver parameters to be accepted: %v", err)
	}
}

func TestNewExecutorFromPreparedRejectsMismatchedMethodReceiverName(t *testing.T) {
	prepared := &PreparedProgram{
		Functions: map[string]*PreparedFunction{
			"Alias.Call": {
				Name:        "Alias.Call",
				Receiver:    TypeSpec("Box"),
				FunctionSig: MustParseRuntimeFuncSig("function(Ptr<Box>) Void"),
			},
		},
	}
	_, err := NewExecutorFromPrepared(prepared)
	if err == nil {
		t.Fatal("expected receiver name mismatch error")
	}
	if !strings.Contains(err.Error(), "receiver name Alias does not match receiver Box") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewExecutorFromPreparedRejectsPointerMethodReceiverMetadata(t *testing.T) {
	prepared := &PreparedProgram{
		Functions: map[string]*PreparedFunction{
			"Box.Call": {
				Name:        "Box.Call",
				Receiver:    TypeSpec("Ptr<Box>"),
				FunctionSig: MustParseRuntimeFuncSig("function(Ptr<Box>) Void"),
			},
		},
	}
	_, err := NewExecutorFromPrepared(prepared)
	if err == nil {
		t.Fatal("expected pointer receiver metadata error")
	}
	if !strings.Contains(err.Error(), "receiver must be a named type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewExecutorFromPreparedRejectsPrimitiveMethodReceiverMetadata(t *testing.T) {
	prepared := &PreparedProgram{
		Functions: map[string]*PreparedFunction{
			"Int64.Call": {
				Name:        "Int64.Call",
				Receiver:    TypeSpec("Int64"),
				FunctionSig: MustParseRuntimeFuncSig("function(Int64) Void"),
			},
		},
	}
	_, err := NewExecutorFromPrepared(prepared)
	if err == nil {
		t.Fatal("expected primitive receiver metadata error")
	}
	if !strings.Contains(err.Error(), "receiver must be a named type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalPreparedFunctionRejectsMethodReceiver(t *testing.T) {
	exec := newEmptyExecutor(t)
	fn := &PreparedFunction{
		Name:        "Box.Call",
		Receiver:    TypeSpec("Box"),
		FunctionSig: MustParseRuntimeFuncSig("function(Ptr<Box>) Void"),
	}
	_, err := exec.EvalPreparedFunction(context.Background(), fn, nil)
	if err == nil {
		t.Fatal("expected method receiver rejection")
	}
	if !strings.Contains(err.Error(), "does not accept method receiver") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewExecutorFromPreparedRejectsDuplicateMethodBinding(t *testing.T) {
	prepared := &PreparedProgram{
		Package: "pkg",
		Functions: map[string]*PreparedFunction{
			"Box.Call": {
				Name:        "Box.Call",
				Receiver:    TypeSpec("Box"),
				FunctionSig: MustParseRuntimeFuncSig("function(Ptr<Box>) Void"),
			},
			"pkg.Box.Call": {
				Name:        "pkg.Box.Call",
				Receiver:    TypeSpec("pkg.Box"),
				FunctionSig: MustParseRuntimeFuncSig("function(Ptr<pkg.Box>) Void"),
			},
		},
	}
	_, err := NewExecutorFromPrepared(prepared)
	if err == nil {
		t.Fatal("expected duplicate method binding error")
	}
	if !strings.Contains(err.Error(), "registered by both") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRuntimeScopeExitReturnsErrorInsteadOfPanic(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.TaskStack = []Task{{Op: OpScopeExit}}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("scope exit should return an error, got panic: %v", r)
		}
	}()

	err := exec.Run(session)
	if err == nil {
		t.Fatal("expected scope exit error")
	}
	if !strings.Contains(err.Error(), "scope exit would leave root scope") {
		t.Fatalf("unexpected error: %v", err)
	}
}
