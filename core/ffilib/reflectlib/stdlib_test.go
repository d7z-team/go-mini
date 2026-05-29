package reflectlib

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/reflectspec"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestReflectSurfaceFollowsSharedSpec(t *testing.T) {
	bundle := SurfaceReflect()
	if bundle == nil || bundle.Schema == nil {
		t.Fatal("expected reflect surface schema")
	}

	routeNames := make(map[string]struct{})
	methodIDs := make(map[uint32]string)
	for i, decl := range reflectspec.PackageFunctions() {
		if want := uint32(i + 1); decl.MethodID != want {
			t.Fatalf("package function %s method id = %d, want %d", decl.RouteName, decl.MethodID, want)
		}
	}
	for _, decl := range reflectspec.Routes() {
		if _, ok := routeNames[decl.RouteName]; ok {
			t.Fatalf("duplicate reflect route name %s", decl.RouteName)
		}
		routeNames[decl.RouteName] = struct{}{}
		if prev, ok := methodIDs[decl.MethodID]; ok {
			t.Fatalf("duplicate reflect method id %d for %s and %s", decl.MethodID, prev, decl.RouteName)
		}
		methodIDs[decl.MethodID] = decl.RouteName

		wantSig := runtime.MustParseRuntimeFuncSig(decl.Signature())
		if decl.TypeOwner != (reflectspec.Owner{}) {
			typeName := decl.TypeOwner.TypeName()
			typ := bundle.Schema.Types[typeName.String()]
			if typ == nil || typ.Methods[decl.MethodName] == nil {
				t.Fatalf("missing reflected type method schema %s.%s", typeName, decl.MethodName)
			}
			method := typ.Methods[decl.MethodName]
			if method.RouteName != decl.RouteName || method.MethodID != decl.MethodID || !runtime.SameRuntimeFuncSchema(method.Sig, wantSig) {
				t.Fatalf("bad reflected type method schema %s: %#v", decl.RouteName, method)
			}
			continue
		}

		pkg := bundle.Schema.Packages[decl.PackagePath]
		if pkg == nil || pkg.Members[decl.MemberName] == nil || pkg.Members[decl.MemberName].Route == nil {
			t.Fatalf("missing reflected package function schema %s.%s", decl.PackagePath, decl.MemberName)
		}
		route := pkg.Members[decl.MemberName].Route
		if route.RouteName != decl.RouteName || route.MethodID != decl.MethodID || !runtime.SameRuntimeFuncSchema(route.Sig, wantSig) {
			t.Fatalf("bad reflected package function schema %s: %#v", decl.RouteName, route)
		}
	}

	bound, err := bundle.Bind(runtime.FFIBindContext{})
	if err != nil {
		t.Fatalf("bind reflect surface failed: %v", err)
	}
	for name := range routeNames {
		route, ok := bound.Routes[name]
		if !ok {
			t.Fatalf("missing native reflect route %s", name)
		}
		if route.Native == nil || route.Bridge != nil {
			t.Fatalf("route %s should be native-only: %#v", name, route)
		}
	}
}
