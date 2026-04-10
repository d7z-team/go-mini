package runtime

import (
	"context"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

func newEmptyExecutor(t *testing.T) *Executor {
	t.Helper()
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})
	return exec
}

func TestStackContextLocalSymbolSlots(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.ScopeApply("fn")

	sym := SymbolRef{Name: "value", Kind: SymbolLocal, Slot: 0}
	if err := session.DeclareSymbol(sym, MustParseRuntimeType("Int64")); err != nil {
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

	if err := session.NewVar("globalValue", MustParseRuntimeType("Int64")); err != nil {
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
	globalValue, ok := session.Shared.LoadGlobal("globalValue")
	if !ok || globalValue == nil || globalValue.I64 != 33 {
		t.Fatalf("expected dedicated global store entry, got %#v", globalValue)
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
	if err := session.DeclareSymbol(local, MustParseRuntimeType("Int64")); err != nil {
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

func TestCaptureSymbolForUpvalueForwardsSharedCellAcrossNestedClosures(t *testing.T) {
	exec := newEmptyExecutor(t)
	outer := exec.NewSession(context.Background(), "global")
	outer.ScopeApply("outer")

	local := SymbolRef{Name: "counter", Kind: SymbolLocal, Slot: 0}
	if err := outer.DeclareSymbol(local, MustParseRuntimeType("Int64")); err != nil {
		t.Fatalf("declare local symbol failed: %v", err)
	}
	if err := outer.StoreSymbol(local, NewInt(1)); err != nil {
		t.Fatalf("store local symbol failed: %v", err)
	}

	cell, err := outer.CaptureSymbol(local)
	if err != nil {
		t.Fatalf("capture local symbol failed: %v", err)
	}

	mid := &StackContext{
		Context:  context.Background(),
		Executor: exec,
		Stack: &Stack{
			Parent: outer.Stack,
			Frame: &SlotFrame{
				Upvalues:     []*Var{cell},
				UpvalueNames: []string{"counter"},
			},
			Scope: "mid",
			Depth: outer.Stack.Depth + 1,
		},
		ValueStack: &ValueStack{},
		LHSStack:   &LHSStack{},
	}
	upvalue := SymbolRef{Name: "counter", Kind: SymbolUpvalue, Slot: 0}

	forwarded, err := mid.CaptureSymbol(upvalue)
	if err != nil {
		t.Fatalf("capture forwarded upvalue failed: %v", err)
	}
	if forwarded != cell {
		t.Fatalf("expected nested capture to reuse shared cell: outer=%p mid=%p", cell, forwarded)
	}

	inner := &StackContext{
		Context:  context.Background(),
		Executor: exec,
		Stack: &Stack{
			Parent: mid.Stack,
			Frame: &SlotFrame{
				Upvalues:     []*Var{forwarded},
				UpvalueNames: []string{"counter"},
			},
			Scope: "inner",
			Depth: mid.Stack.Depth + 1,
		},
		ValueStack: &ValueStack{},
		LHSStack:   &LHSStack{},
	}

	if err := inner.StoreSymbol(upvalue, NewInt(5)); err != nil {
		t.Fatalf("store nested upvalue failed: %v", err)
	}

	got, err := outer.LoadSymbol(local)
	if err != nil {
		t.Fatalf("load outer local after nested write failed: %v", err)
	}
	if got == nil || got.I64 != 5 {
		t.Fatalf("expected nested upvalue write to reach outer local, got %#v", got)
	}
}

func TestStackContextReturnSlot(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.ScopeApply("fn")

	if err := session.InitReturn(MustParseRuntimeType("Int64")); err != nil {
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

func TestReturnSlotAPIsRequireFrameSlot(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	if _, err := session.LoadReturn(); err == nil {
		t.Fatal("expected LoadReturn to fail without frame slot")
	}
	if err := session.StoreReturn(NewInt(1)); err == nil {
		t.Fatal("expected StoreReturn to fail without frame slot")
	}
	if _, exists := session.Stack.MemoryPtr["__return__"]; exists {
		t.Fatalf("return slot API should not recreate MemoryPtr fallback: %#v", session.Stack.MemoryPtr["__return__"])
	}
}

func TestSetupFuncCallInitializesParamSlots(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	err := exec.setupFuncCall(session, "sum", &DoCallData{
		Name: "sum",
		FunctionSig: MustParseRuntimeFuncSig("function(Int64, Int64) Int64"),
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

	if a == nil || a.I64 != 3 || b == nil || b.I64 != 4 || ret == nil || ret.RawType() != "Int64" {
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
	if err := session.DeclareSymbol(local, MustParseRuntimeType("Int64")); err != nil {
		t.Fatalf("declare local symbol failed: %v", err)
	}
	if err := session.StoreSymbol(local, NewInt(9)); err != nil {
		t.Fatalf("store local symbol failed: %v", err)
	}
	if err := session.InitReturn(MustParseRuntimeType("Int64")); err != nil {
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
	if err := session.DeclareSymbol(local, MustParseRuntimeType("Int64")); err != nil {
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
	if err := session.DeclareSymbol(local, MustParseRuntimeType("Int64")); err != nil {
		t.Fatalf("declare local symbol failed: %v", err)
	}
	if err := session.NewVar("value", MustParseRuntimeType("String")); err != nil {
		t.Fatalf("new var by name failed: %v", err)
	}
	got, err := session.LoadSymbol(local)
	if err != nil {
		t.Fatalf("load local slot failed: %v", err)
	}
	if got == nil || got.RawType() != "Int64" {
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
	session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: []*Var{NewInt(7)}}, TypeInfo: MustParseRuntimeType("[]Int64")})

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
		FunctionSig: MustParseRuntimeFuncSig("function() Int64"),
	}, nil, nil); err != nil {
		t.Fatalf("setup func call failed: %v", err)
	}

	session.ValueStack.Push(NewInt(99))
	session.LHSStack.Push(&LHSMember{Obj: &Var{VType: TypeMap, Ref: &VMMap{Data: map[string]*Var{"x": NewInt(1)}}}, Property: "x"})
	if err := session.StoreReturn(NewInt(7)); err != nil {
		t.Fatalf("store return failed: %v", err)
	}

	if err := exec.dispatch(session, Task{Op: OpCallBoundary, Data: map[string]interface{}{
		"oldStack": outerStack,
	}}); err == nil {
		t.Fatal("expected legacy map payload to be rejected")
	}

	if err := exec.dispatch(session, Task{Op: OpCallBoundary, Data: &CallBoundaryData{
		Name:      "sum",
		OldStack:  outerStack,
		HasReturn: true,
		ValueBase: 1,
		LHSBase:   1,
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

func TestResolveAddressSupportsLoadStoreAndUpdate(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.ScopeApply("fn")

	local := SymbolRef{Name: "n", Kind: SymbolLocal, Slot: 0}
	if err := session.DeclareSymbol(local, MustParseRuntimeType("Int64")); err != nil {
		t.Fatalf("declare local symbol failed: %v", err)
	}
	if err := exec.assignAddress(session, &LHSEnv{Name: "n", Sym: local}, NewInt(4)); err != nil {
		t.Fatalf("assign address failed: %v", err)
	}
	if err := exec.updateAddress(session, &LHSEnv{Name: "n", Sym: local}, "++"); err != nil {
		t.Fatalf("update address failed: %v", err)
	}
	got, err := exec.loadAddress(session, &LHSEnv{Name: "n", Sym: local})
	if err != nil {
		t.Fatalf("load address failed: %v", err)
	}
	if got == nil || got.I64 != 5 {
		t.Fatalf("unexpected local address value: %#v", got)
	}

	arr := &Var{VType: TypeArray, Ref: &VMArray{Data: []*Var{NewInt(1), NewInt(2)}}, TypeInfo: MustParseRuntimeType("[]Int64")}
	index := &LHSIndex{Obj: arr, Index: NewInt(1)}
	if err := exec.assignAddress(session, index, NewInt(9)); err != nil {
		t.Fatalf("assign indexed address failed: %v", err)
	}
	if err := exec.updateAddress(session, index, "--"); err != nil {
		t.Fatalf("update indexed address failed: %v", err)
	}
	got, err = exec.loadAddress(session, index)
	if err != nil {
		t.Fatalf("load indexed address failed: %v", err)
	}
	if got == nil || got.I64 != 8 {
		t.Fatalf("unexpected indexed address value: %#v", got)
	}
}

func TestResolveAddressSupportsAnyWrappedMapMemberAndDereferenceTargets(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	innerMap := &Var{
		VType: TypeMap,
		TypeInfo: MustParseRuntimeType("Map<String,Int64>"),
		Ref:   &VMMap{Data: map[string]*Var{"count": NewInt(1)}},
	}
	anyMap := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: innerMap}
	member := &LHSMember{Obj: anyMap, Property: "count"}

	if err := exec.assignAddress(session, member, NewInt(9)); err != nil {
		t.Fatalf("assign wrapped member failed: %v", err)
	}
	got, err := exec.loadAddress(session, member)
	if err != nil {
		t.Fatalf("load wrapped member failed: %v", err)
	}
	if got == nil || got.I64 != 9 {
		t.Fatalf("unexpected wrapped member value: %#v", got)
	}

	ptr := &Var{VType: TypeHandle, Handle: 7, TypeInfo: MustParseRuntimeType("Ptr<Int64>"), Ref: NewInt(3)}
	anyPtr := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: &Var{VType: TypeCell, Ref: &Cell{Value: ptr}}}
	deref := &LHSDeref{Target: anyPtr}

	if err := exec.assignAddress(session, deref, NewInt(11)); err != nil {
		t.Fatalf("assign wrapped dereference failed: %v", err)
	}
	if err := exec.updateAddress(session, deref, "++"); err != nil {
		t.Fatalf("update wrapped dereference failed: %v", err)
	}
	got, err = exec.loadAddress(session, deref)
	if err != nil {
		t.Fatalf("load wrapped dereference failed: %v", err)
	}
	if got == nil || got.I64 != 12 {
		t.Fatalf("unexpected wrapped dereference value: %#v", got)
	}
}
