package runtime

import (
	"context"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

func newEmptyExecutor(t *testing.T) *Executor {
	t.Helper()
	exec, err := NewExecutor(&ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})
	if err != nil {
		t.Fatalf("new executor failed: %v", err)
	}
	return exec
}

func TestStackContextLocalSymbolSlots(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.ScopeApply("fn")

	sym := SymbolRef{Name: "value", Kind: SymbolLocal, Slot: 0}
	if err := session.DeclareSymbol(sym, "Int64"); err != nil {
		t.Fatalf("declare local symbol failed: %v", err)
	}
	if err := session.StoreSymbol(sym, NewInt(41)); err != nil {
		t.Fatalf("store local symbol failed: %v", err)
	}

	got, err := session.LoadSymbol(sym)
	if err != nil {
		t.Fatalf("load local symbol failed: %v", err)
	}
	if got == nil || got.I64 != 41 {
		t.Fatalf("unexpected local slot value: %#v", got)
	}

	session.ScopeApply("inner")
	got, err = session.LoadSymbol(sym)
	if err != nil {
		t.Fatalf("load local symbol from child scope failed: %v", err)
	}
	if got == nil || got.I64 != 41 {
		t.Fatalf("unexpected child-scope local slot value: %#v", got)
	}
	if _, exists := session.Stack.Parent.MemoryPtr["value"]; exists {
		t.Fatalf("local slot should not require MemoryPtr mirror: %#v", session.Stack.Parent.MemoryPtr["value"])
	}
}

func TestGlobalBindingsUseDedicatedGlobalStore(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	if err := session.NewVar("globalValue", "Int64"); err != nil {
		t.Fatalf("new global failed: %v", err)
	}
	if err := session.Store("globalValue", NewInt(33)); err != nil {
		t.Fatalf("store global failed: %v", err)
	}

	got, err := session.Load("globalValue")
	if err != nil {
		t.Fatalf("load global failed: %v", err)
	}
	if got == nil || got.I64 != 33 {
		t.Fatalf("unexpected global value: %#v", got)
	}
	if _, exists := session.Stack.MemoryPtr["globalValue"]; exists {
		t.Fatalf("global should not require MemoryPtr storage: %#v", session.Stack.MemoryPtr["globalValue"])
	}
	if session.Stack.Globals["globalValue"] == nil || session.Stack.Globals["globalValue"].I64 != 33 {
		t.Fatalf("expected dedicated global store entry, got %#v", session.Stack.Globals["globalValue"])
	}
}

func TestGlobalSymbolUsesDedicatedGlobalStore(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	sym := SymbolRef{Name: "shared", Kind: SymbolGlobal, Slot: -1}

	if err := session.StoreSymbol(sym, NewInt(8)); err != nil {
		t.Fatalf("store global symbol failed: %v", err)
	}
	got, err := session.LoadSymbol(sym)
	if err != nil {
		t.Fatalf("load global symbol failed: %v", err)
	}
	if got == nil || got.I64 != 8 {
		t.Fatalf("unexpected global symbol value: %#v", got)
	}

	session.ScopeApply("child")
	got, err = session.LoadSymbol(sym)
	if err != nil {
		t.Fatalf("load global symbol from child failed: %v", err)
	}
	if got == nil || got.I64 != 8 {
		t.Fatalf("unexpected child global symbol value: %#v", got)
	}
	if _, exists := session.Stack.MemoryPtr["shared"]; exists {
		t.Fatalf("global symbol should not require child MemoryPtr mirror: %#v", session.Stack.MemoryPtr["shared"])
	}
}

func TestStackContextUpvalueSlotsShareCapturedCell(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.ScopeApply("fn")

	local := SymbolRef{Name: "counter", Kind: SymbolLocal, Slot: 0}
	if err := session.DeclareSymbol(local, "Int64"); err != nil {
		t.Fatalf("declare local symbol failed: %v", err)
	}
	if err := session.StoreSymbol(local, NewInt(1)); err != nil {
		t.Fatalf("store local symbol failed: %v", err)
	}

	cell, err := session.CaptureSymbol(local)
	if err != nil {
		t.Fatalf("capture local symbol failed: %v", err)
	}

	closureSession := &StackContext{
		Context:  context.Background(),
		Executor: exec,
		Stack: &Stack{
			Parent:    session.Stack,
			MemoryPtr: map[string]*Var{"counter": cell},
			Frame: &SlotFrame{
				Upvalues:     []*Var{cell},
				UpvalueNames: []string{"counter"},
			},
			Scope: "closure",
			Depth: session.Stack.Depth + 1,
		},
		ValueStack: &ValueStack{},
		LHSStack:   &LHSStack{},
	}
	upvalue := SymbolRef{Name: "counter", Kind: SymbolUpvalue, Slot: 0}

	if err := closureSession.StoreSymbol(upvalue, NewInt(2)); err != nil {
		t.Fatalf("store upvalue symbol failed: %v", err)
	}

	got, err := closureSession.LoadSymbol(upvalue)
	if err != nil {
		t.Fatalf("load upvalue symbol failed: %v", err)
	}
	if got == nil || got.I64 != 2 {
		t.Fatalf("unexpected upvalue slot value: %#v", got)
	}

	localVal, err := session.LoadSymbol(local)
	if err != nil {
		t.Fatalf("load local symbol after upvalue write failed: %v", err)
	}
	if localVal == nil || localVal.I64 != 2 {
		t.Fatalf("expected captured cell to share state, got %#v", localVal)
	}
	if _, exists := session.Stack.MemoryPtr["counter"]; exists {
		t.Fatalf("captured local should not require MemoryPtr mirror: %#v", session.Stack.MemoryPtr["counter"])
	}
}

func TestStackContextReturnSlot(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.ScopeApply("fn")

	if err := session.InitReturn("Int64"); err != nil {
		t.Fatalf("init return slot failed: %v", err)
	}
	if err := session.StoreReturn(NewInt(7)); err != nil {
		t.Fatalf("store return slot failed: %v", err)
	}

	got, err := session.LoadReturn()
	if err != nil {
		t.Fatalf("load return slot failed: %v", err)
	}
	if got == nil || got.I64 != 7 {
		t.Fatalf("unexpected return slot value: %#v", got)
	}
	if _, exists := session.Stack.MemoryPtr["__return__"]; exists {
		t.Fatalf("return slot should not require MemoryPtr mirror: %#v", session.Stack.MemoryPtr["__return__"])
	}
}

func TestSetupFuncCallInitializesParamSlots(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	err := exec.setupFuncCall(session, "sum", &DoCallData{
		Name: "sum",
		FunctionType: ast.FunctionType{
			Params: []ast.FunctionParam{
				{Name: "a", Type: "Int64"},
				{Name: "b", Type: "Int64"},
			},
			Return: "Int64",
		},
	}, []*Var{NewInt(3), NewInt(4)}, nil)
	if err != nil {
		t.Fatalf("setup func call failed: %v", err)
	}

	a, err := session.LoadSymbol(SymbolRef{Name: "a", Kind: SymbolLocal, Slot: 0})
	if err != nil {
		t.Fatalf("load param a failed: %v", err)
	}
	b, err := session.LoadSymbol(SymbolRef{Name: "b", Kind: SymbolLocal, Slot: 1})
	if err != nil {
		t.Fatalf("load param b failed: %v", err)
	}
	ret, err := session.LoadReturn()
	if err != nil {
		t.Fatalf("load return slot failed: %v", err)
	}

	if a == nil || a.I64 != 3 || b == nil || b.I64 != 4 || ret == nil || ret.Type != "Int64" {
		t.Fatalf("unexpected function frame state: a=%#v b=%#v ret=%#v", a, b, ret)
	}
	if _, exists := session.Stack.MemoryPtr["a"]; exists {
		t.Fatalf("param slot should not require MemoryPtr mirror: %#v", session.Stack.MemoryPtr["a"])
	}
}

func TestDumpVariablesIncludesSlotFrameValues(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.ScopeApply("fn")

	local := SymbolRef{Name: "value", Kind: SymbolLocal, Slot: 0}
	if err := session.DeclareSymbol(local, "Int64"); err != nil {
		t.Fatalf("declare local symbol failed: %v", err)
	}
	if err := session.StoreSymbol(local, NewInt(9)); err != nil {
		t.Fatalf("store local symbol failed: %v", err)
	}
	if err := session.InitReturn("Int64"); err != nil {
		t.Fatalf("init return slot failed: %v", err)
	}
	if err := session.StoreReturn(NewInt(12)); err != nil {
		t.Fatalf("store return slot failed: %v", err)
	}

	vars := session.Stack.DumpVariables()
	if vars["value"] != "9" || vars["__return__"] != "12" {
		t.Fatalf("unexpected dumped variables: %#v", vars)
	}
}

func TestLookupFrameVarByNameUsesSlotIndexes(t *testing.T) {
	frame := &SlotFrame{}
	frame.ensureLocalSlot(2, "local")
	frame.Locals[2] = NewInt(5)
	frame.ensureUpvalueSlot(1, "captured")
	frame.Upvalues[1] = NewInt(8)
	frame.ReturnName = "__return__"
	frame.Return = NewInt(13)

	if got := lookupFrameVarByName(frame, "local"); got == nil || got.I64 != 5 {
		t.Fatalf("unexpected local lookup result: %#v", got)
	}
	if got := lookupFrameVarByName(frame, "captured"); got == nil || got.I64 != 8 {
		t.Fatalf("unexpected upvalue lookup result: %#v", got)
	}
	if got := lookupFrameVarByName(frame, "__return__"); got == nil || got.I64 != 13 {
		t.Fatalf("unexpected return lookup result: %#v", got)
	}
}

func TestNameAPIsPreferFrameSymbols(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.ScopeApply("fn")

	local := SymbolRef{Name: "value", Kind: SymbolLocal, Slot: 0}
	if err := session.DeclareSymbol(local, "Int64"); err != nil {
		t.Fatalf("declare local symbol failed: %v", err)
	}
	if err := session.Store("value", NewInt(21)); err != nil {
		t.Fatalf("store by name failed: %v", err)
	}
	got, err := session.Load("value")
	if err != nil {
		t.Fatalf("load by name failed: %v", err)
	}
	if got == nil || got.I64 != 21 {
		t.Fatalf("unexpected named load result: %#v", got)
	}
	cell, err := session.CaptureVar("value")
	if err != nil {
		t.Fatalf("capture by name failed: %v", err)
	}
	if cell == nil || cell.VType != TypeCell {
		t.Fatalf("expected captured cell, got %#v", cell)
	}
	if _, exists := session.Stack.MemoryPtr["value"]; exists {
		t.Fatalf("name APIs should not recreate local MemoryPtr mirror: %#v", session.Stack.MemoryPtr["value"])
	}
}

func TestNewVarDoesNotShadowFrameSlot(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.ScopeApply("fn")

	local := SymbolRef{Name: "value", Kind: SymbolLocal, Slot: 0}
	if err := session.DeclareSymbol(local, "Int64"); err != nil {
		t.Fatalf("declare local symbol failed: %v", err)
	}
	if err := session.NewVar("value", "String"); err != nil {
		t.Fatalf("new var by name failed: %v", err)
	}
	got, err := session.LoadSymbol(local)
	if err != nil {
		t.Fatalf("load local slot failed: %v", err)
	}
	if got == nil || got.Type != "Int64" {
		t.Fatalf("frame slot should remain intact, got %#v", got)
	}
	if _, exists := session.Stack.MemoryPtr["value"]; exists {
		t.Fatalf("new var should not create MemoryPtr mirror for frame slot: %#v", session.Stack.MemoryPtr["value"])
	}
}

func TestNilIsBuiltinRuntimeValue(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	got, err := session.Load("nil")
	if err != nil {
		t.Fatalf("load nil failed: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil runtime value, got %#v", got)
	}
	if _, exists := session.Stack.MemoryPtr["nil"]; exists {
		t.Fatalf("nil should not require MemoryPtr entry")
	}
}

func TestOpEvalLHSPushesAddressOntoLHSStack(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.ValueStack.Push(NewInt(1))
	session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: []*Var{NewInt(7)}}, Type: "[]Int64"})

	if err := exec.dispatch(session, Task{Op: OpEvalLHS, Data: &LHSData{Kind: LHSTypeIndex}}); err != nil {
		t.Fatalf("eval lhs failed: %v", err)
	}

	if session.ValueStack.Len() != 0 {
		t.Fatalf("OpEvalLHS should not leave descriptor on ValueStack")
	}
	if session.LHSStack.Len() != 1 {
		t.Fatalf("expected one address on LHSStack, got %d", session.LHSStack.Len())
	}
	if _, ok := session.LHSStack.Peek().(*LHSIndex); !ok {
		t.Fatalf("expected indexed LHS descriptor, got %#v", session.LHSStack.Peek())
	}
}

func TestOpCallBoundaryTruncatesTemporaryStacks(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	outerStack := session.Stack
	session.ValueStack.Push(NewInt(10))
	session.LHSStack.Push(&LHSEnv{Name: "slot"})

	if err := exec.setupFuncCall(session, "sum", &DoCallData{
		Name: "sum",
		FunctionType: ast.FunctionType{
			Return: "Int64",
		},
	}, nil, nil); err != nil {
		t.Fatalf("setup func call failed: %v", err)
	}

	session.ValueStack.Push(NewInt(99))
	session.LHSStack.Push(&LHSMember{Obj: &Var{VType: TypeMap, Ref: &VMMap{Data: map[string]*Var{"x": NewInt(1)}}}, Property: "x"})
	if err := session.StoreReturn(NewInt(7)); err != nil {
		t.Fatalf("store return failed: %v", err)
	}

	if err := exec.dispatch(session, Task{Op: OpCallBoundary, Data: map[string]interface{}{
		"oldStack":  outerStack,
		"hasReturn": true,
		"valueBase": 1,
		"lhsBase":   1,
	}}); err != nil {
		t.Fatalf("call boundary failed: %v", err)
	}

	if session.Stack != outerStack {
		t.Fatal("expected call boundary to restore outer stack")
	}
	if session.ValueStack.Len() != 2 {
		t.Fatalf("expected preserved outer value plus return, got %d", session.ValueStack.Len())
	}
	ret := session.ValueStack.Pop()
	if ret == nil || ret.I64 != 7 {
		t.Fatalf("unexpected return value after boundary: %#v", ret)
	}
	if session.ValueStack.Len() != 1 {
		t.Fatalf("expected temporary call values to be truncated, got %d", session.ValueStack.Len())
	}
	if session.LHSStack.Len() != 1 {
		t.Fatalf("expected temporary LHS values to be truncated, got %d", session.LHSStack.Len())
	}
	if _, ok := session.LHSStack.Peek().(*LHSEnv); !ok {
		t.Fatalf("unexpected outer LHS stack entry: %#v", session.LHSStack.Peek())
	}
}
