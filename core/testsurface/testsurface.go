package testsurface

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

type User interface {
	UseSurface(*surface.Bundle) error
}

func Route(fullName string, methodID uint32, sig *runtime.RuntimeFuncSig, doc string) runtime.FFIRouteDecl {
	pkg, member := runtime.SplitExternalName(fullName)
	if pkg == "" || member == "" {
		panic("test FFI route must use package.member name: " + fullName)
	}
	return runtime.FFIRouteDecl{
		PackagePath: pkg,
		MemberName:  member,
		RouteName:   fullName,
		MethodID:    methodID,
		Sig:         sig,
		Doc:         doc,
	}
}

func Routes(bridge ffigo.FFIBridge, routes ...runtime.FFIRouteDecl) *surface.Bundle {
	return surface.Routes(bridge, routes...)
}

func RouteBundle(fullName string, bridge ffigo.FFIBridge, methodID uint32, sig *runtime.RuntimeFuncSig, doc string) *surface.Bundle {
	return Routes(bridge, Route(fullName, methodID, sig, doc))
}

func SchemaBundle(schema *runtime.FFISurfaceSchema, bridge ffigo.FFIBridge) *surface.Bundle {
	return surface.Router(schema, bridge)
}

func UseRoute(tb testing.TB, user User, fullName string, bridge ffigo.FFIBridge, methodID uint32, sig *runtime.RuntimeFuncSig, doc string) {
	tb.Helper()
	if err := user.UseSurface(RouteBundle(fullName, bridge, methodID, sig, doc)); err != nil {
		tb.Fatalf("UseSurface(%s) failed: %v", fullName, err)
	}
}

func UseRoutes(tb testing.TB, user User, bridge ffigo.FFIBridge, routes ...runtime.FFIRouteDecl) {
	tb.Helper()
	if err := user.UseSurface(Routes(bridge, routes...)); err != nil {
		tb.Fatalf("UseSurface routes failed: %v", err)
	}
}
