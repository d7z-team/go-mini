package tests

import (
	"context"
	goerrors "errors"
	"fmt"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

func TestFFIHostErrorUsesGoErrorsSemantics(t *testing.T) {
	hostSurface := hostErrorSurface()
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("hosterr", hostSurface),
	}, []testutil.Case{
		{
			Name:    "host-error-is-and-identity",
			Imports: []string{"errors", "hosterr"},
			Body: `
target := hosterr.Target()
again := hosterr.Target()
wrapped := hosterr.Wrapped()
other := hosterr.Other()

test.Out(target.Error())
test.Out("|")
test.OutBool(errors.Is(wrapped, target))
test.Out("|")
test.OutBool(errors.Is(target, again))
test.Out("|")
test.OutBool(target == again)
test.Out("|")
test.OutBool(errors.Is(wrapped, other))
`,
			Want:   "host-root|true|true|false|false",
			Covers: []string{"Target", "Wrapped", "Other"},
		},
	}, testutil.WithSurface(hostSurface))
}

func hostErrorSurface() *surface.Bundle {
	registry := ffigo.NewHandleRegistry()
	base := goerrors.New("host-root")
	wrapped := fmt.Errorf("host-wrap: %w", base)
	other := goerrors.New("host-root")
	bridge := ffigo.NewRouterBridge(registry, func(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
		var err error
		switch req.MethodID {
		case 1:
			err = base
		case 2:
			err = wrapped
		case 3:
			err = other
		default:
			return nil, fmt.Errorf("unknown method %d", req.MethodID)
		}
		buf := ffigo.GetBuffer()
		defer ffigo.ReleaseBuffer(buf)
		buf.WriteRawError(err.Error(), registry.Register(err))
		return append([]byte(nil), buf.Bytes()...), nil
	})

	sig := runtime.MustRuntimeFuncSig(runtime.SpecError, false)
	schema := runtime.NewFFISurfaceSchema()
	for _, route := range []struct {
		member   string
		route    string
		methodID uint32
	}{
		{"Target", "hosterr.Target", 1},
		{"Wrapped", "hosterr.Wrapped", 2},
		{"Other", "hosterr.Other", 3},
	} {
		if err := schema.AddFunc("hosterr", route.member, route.route, route.methodID, sig, ""); err != nil {
			return &surface.Bundle{Err: err}
		}
	}
	return surface.New(schema, func(_ runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		bound := runtime.NewBoundFFISurface(schema)
		bound.AddRoute("hosterr", "Target", runtime.FFIRoute{Name: "hosterr.Target", Bridge: bridge, MethodID: 1, FuncSig: sig})
		bound.AddRoute("hosterr", "Wrapped", runtime.FFIRoute{Name: "hosterr.Wrapped", Bridge: bridge, MethodID: 2, FuncSig: sig})
		bound.AddRoute("hosterr", "Other", runtime.FFIRoute{Name: "hosterr.Other", Bridge: bridge, MethodID: 3, FuncSig: sig})
		return bound, nil
	})
}
