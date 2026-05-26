package runtime

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

func TestCheckSatisfactionBuildsInterfaceVTable(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types: map[ast.Ident]ast.GoMiniType{
			"Closer": "interface{Close() Error; Read() tuple(Int64, Error);}",
		},
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	obj := &Var{
		VType:    TypeMap,
		TypeInfo: MustParseRuntimeType("demo.Reader"),
		Ref: &VMMap{Data: map[string]*Var{
			"Close": {
				VType: TypeClosure,
				Ref:   &VMClosure{FunctionSig: MustParseRuntimeFuncSig("function() Error")},
			},
			"Read": {
				VType: TypeClosure,
				Ref:   &VMClosure{FunctionSig: MustParseRuntimeFuncSig("function() tuple(Int64, Error)")},
			},
		}},
	}

	wrapped, err := exec.CheckSatisfaction(obj, "Closer")
	if err != nil {
		t.Fatalf("CheckSatisfaction failed: %v", err)
	}
	if wrapped.VType != TypeInterface {
		t.Fatalf("expected interface wrapper, got %#v", wrapped)
	}
	iface := wrapped.Ref.(*VMInterface)
	if iface.Spec == nil || len(iface.VTable) != len(iface.Spec.Methods) {
		t.Fatalf("unexpected interface metadata: %#v", iface)
	}
	closeIdx := iface.Spec.MethodIndex["Close"]
	readIdx := iface.Spec.MethodIndex["Read"]
	if iface.VTable[closeIdx] == nil || iface.VTable[readIdx] == nil {
		t.Fatalf("expected populated vtable, got %#v", iface.VTable)
	}

	closeVal, err := exec.evalMemberExprDirect(nil, wrapped, "Close")
	if err != nil {
		t.Fatalf("evalMemberExprDirect failed: %v", err)
	}
	if closeVal != iface.VTable[closeIdx] {
		t.Fatalf("expected vtable-backed method lookup")
	}
}

func TestResolveStructSchemaUsesCanonicalTypeID(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	spec := MustParseRuntimeStructSpec("demo.Type", StructOwnershipVMValue, "struct { Value Int64; }")
	exec.metadata.registerStructSchema("demo.Type", spec)

	resolved, ok := exec.resolveStructSchema("Ptr<demo.Type>")
	if !ok {
		t.Fatal("expected canonical struct lookup to succeed")
	}
	if resolved.TypeID != "demo.Type" || resolved.Layout.FieldIndex["Value"] != 0 {
		t.Fatalf("unexpected resolved struct schema: %+v", resolved)
	}
}

func TestResolveMethodRouteUsesReceiverMetadata(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		Package:   "context",
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"localType.call": {
				Name:         "call",
				ReceiverType: "localType",
				FunctionType: ast.FunctionType{
					Params: []ast.FunctionParam{{Name: "c", Type: "Ptr<localType>"}},
					Return: ast.TypeVoid,
				},
			},
		},
	})

	exec.routes["demo.Type.Call"] = FFIRoute{
		Name:     "demo.Type.Call",
		MethodID: 1,
		FuncSig:  MustParseRuntimeFuncSig("function(HostRef<demo.Type>) Void"),
	}

	methodName, ok := exec.resolveHostMethodRoute("HostRef<demo.Type>", "Call")
	if !ok {
		t.Fatal("expected dotted method route to resolve")
	}
	if methodName != "demo.Type.Call" {
		t.Fatalf("unexpected method route: %s", methodName)
	}

	methodName, ok = exec.resolveVMMethodRoute("", "call", MustParseRuntimeType("Ptr<context.localType>"))
	if !ok {
		t.Fatal("expected module-qualified VM type method to resolve through receiver metadata")
	}
	if methodName != "localType.call" {
		t.Fatalf("unexpected local method route: %s", methodName)
	}

	if methodName, ok = exec.resolveVMMethodRoute("other.localType", "call"); ok {
		t.Fatalf("qualified foreign receiver resolved through short fallback: %s", methodName)
	}
}

func TestHostRefMethodRouteDoesNotUseVMMethod(t *testing.T) {
	exec := newEmptyExecutor(t)
	exec.functions["demo.Type.Call"] = &RuntimeFunction{
		Name:        "demo.Type.Call",
		Receiver:    TypeSpec("demo.Type"),
		FunctionSig: MustParseRuntimeFuncSig("function(Ptr<demo.Type>) Void"),
	}
	exec.methodFunctions["demo.Type"] = map[string]string{"Call": "demo.Type.Call"}

	if methodName, ok := exec.resolveHostMethodRoute("HostRef<demo.Type>", "Call"); ok {
		t.Fatalf("host receiver resolved VM method without FFI route: %s", methodName)
	}
	if methodName, ok := exec.resolveVMMethodRoute("Ptr<demo.Type>", "Call"); !ok || methodName != "demo.Type.Call" {
		t.Fatalf("VM receiver did not resolve prepared method: %s ok=%t", methodName, ok)
	}
}

func TestMethodClosureCarriesPreparedBody(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})
	exec.functions["localType.call"] = &RuntimeFunction{
		Name:        "localType.call",
		FunctionSig: MustParseRuntimeFuncSig("function(Ptr<localType>) Void"),
		BodyTasks:   []Task{{Op: OpPop}},
	}

	receiver := &Var{VType: TypePointer}
	method := exec.methodClosure(receiver, "localType.call")
	if method == nil || method.VType != TypeClosure {
		t.Fatalf("expected method closure, got %#v", method)
	}
	ref, ok := method.Ref.(*VMMethodValue)
	if !ok {
		t.Fatalf("expected VMMethodValue, got %#v", method.Ref)
	}
	if ref.FuncSig == nil || len(ref.BodyTasks) != 1 || ref.BodyTasks[0].Op != OpPop {
		t.Fatalf("method body was not captured: %#v", ref)
	}
}

func TestResolveNamedTypeDoesNotLoopOnPrimitiveAlias(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types: map[ast.Ident]ast.GoMiniType{
			"MyInt":  "Int64",
			"UserID": "MyInt",
		},
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	res, err := exec.initializeType(nil, MustParseRuntimeType("UserID"), 0)
	if err != nil {
		t.Fatalf("initializeType failed: %v", err)
	}
	if res == nil || res.VType != TypeInt || res.RawType() != "UserID" || res.I64 != 0 {
		t.Fatalf("unexpected initialized alias value: %#v", res)
	}
}

func TestResolveNamedTypeChainDetectsCycles(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types: map[ast.Ident]ast.GoMiniType{
			"A": "B",
			"B": "A",
		},
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	if _, _, err := exec.resolveNamedTypeChain("A"); err == nil {
		t.Fatal("expected named type cycle to be detected")
	}
}

func TestCachedInterfaceSpecReusesParsedLiteral(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	spec1, ok := exec.cachedInterfaceSpec("interface{Close() Error;}")
	if !ok || spec1 == nil {
		t.Fatal("expected cachedInterfaceSpec to parse literal interface")
	}
	spec2, ok := exec.cachedInterfaceSpec("interface{Close() Error;}")
	if !ok || spec2 == nil {
		t.Fatal("expected cachedInterfaceSpec to reuse literal interface")
	}
	if spec1 != spec2 {
		t.Fatal("expected interface literal spec to be cached and reused")
	}
}
