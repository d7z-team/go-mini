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

	methodSig := ast.FunctionType{Return: "Error"}
	readSig := ast.FunctionType{Return: "tuple(Int64, Error)"}
	obj := &Var{
		VType: TypeMap,
		Type:  "demo.Reader",
		Ref: &VMMap{Data: map[string]*Var{
			"Close": {
				VType: TypeClosure,
				Ref:   &VMClosure{FunctionType: methodSig},
			},
			"Read": {
				VType: TypeClosure,
				Ref:   &VMClosure{FunctionType: readSig},
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

	spec := MustParseRuntimeStructSpec("demo.Type", "struct { Value Int64; }")
	exec.RegisterStructSchema("demo.Type", spec)

	resolved, ok := exec.resolveStructSchema("Ptr<demo.Type>")
	if !ok {
		t.Fatal("expected canonical struct lookup to succeed")
	}
	if resolved.TypeID != "demo.Type" || resolved.Layout.FieldIndex["Value"] != 0 {
		t.Fatalf("unexpected resolved struct schema: %+v", resolved)
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

	res := exec.initializeType(nil, "UserID", 0)
	if res == nil || res.VType != TypeInt || res.Type != "UserID" || res.I64 != 0 {
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
