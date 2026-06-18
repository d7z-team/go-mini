package ffilib

import (
	"gopkg.d7z.net/go-mini/core/ffilib/fmtlib"
	"gopkg.d7z.net/go-mini/core/ffilib/mathlib"
	"gopkg.d7z.net/go-mini/core/ffilib/reflectlib"
	"gopkg.d7z.net/go-mini/core/ffilib/sortlib"
	"gopkg.d7z.net/go-mini/core/ffilib/strconvlib"
	"gopkg.d7z.net/go-mini/core/ffilib/stringslib"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

func Surface() *surface.Bundle {
	return surface.Merge(
		nativeErrorSurface(),
		stringslib.SurfaceStrings(&stringslib.StringsHost{}),
		mathlib.SurfaceMath(&mathlib.MathHost{}),
		strconvlib.SurfaceStrconv(&strconvlib.StrconvHost{}),
		sortlib.SurfaceSort(&sortlib.SortHost{}),
		reflectlib.SurfaceReflect(),
		fmtlib.Surface(),
	)
}

func nativeErrorSurface() *surface.Bundle {
	type nativeRoute struct {
		pkg      string
		member   string
		route    string
		methodID uint32
		sig      *runtime.RuntimeFuncSig
		fn       runtime.NativeFunc
		doc      string
	}
	routes := []nativeRoute{
		{
			pkg:      "errors",
			member:   "New",
			route:    "errors.New",
			methodID: 1,
			sig:      runtime.MustRuntimeFuncSig(runtime.SpecError, false, runtime.SpecString),
			fn:       runtime.NativeErrorsNew,
			doc:      "Create a VM error backed by a Go error",
		},
		{
			pkg:      "errors",
			member:   "Is",
			route:    "errors.Is",
			methodID: 2,
			sig:      runtime.MustRuntimeFuncSig(runtime.SpecBool, false, runtime.SpecError, runtime.SpecError),
			fn:       runtime.NativeErrorsIs,
			doc:      "Report whether an error matches a target",
		},
		{
			pkg:      "errors",
			member:   "As",
			route:    "errors.As",
			methodID: 3,
			sig:      runtime.MustRuntimeFuncSig(runtime.SpecBool, false, runtime.SpecError, runtime.SpecAny),
			fn:       runtime.NativeErrorsAs,
			doc:      "Assign the first matching error in an error chain",
		},
		{
			pkg:      "errors",
			member:   "Unwrap",
			route:    "errors.Unwrap",
			methodID: 4,
			sig:      runtime.MustRuntimeFuncSig(runtime.SpecError, false, runtime.SpecError),
			fn:       runtime.NativeErrorsUnwrap,
			doc:      "Return the next wrapped error",
		},
		{
			pkg:      "errors",
			member:   "Stack",
			route:    "errors.Stack",
			methodID: 5,
			sig:      runtime.MustRuntimeFuncSig(runtime.SpecString, false, runtime.SpecError),
			fn:       runtime.NativeErrorsStack,
			doc:      "Return VM stack text attached to an error",
		},
	}
	schema := runtime.NewFFISurfaceSchema()
	for _, r := range routes {
		if err := schema.AddFunc(r.pkg, r.member, r.route, r.methodID, r.sig, r.doc); err != nil {
			return &surface.Bundle{Err: err}
		}
	}
	return surface.New(schema, func(_ runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		bound := runtime.NewBoundFFISurfaceFromSchema(schema)
		natives := make(map[uint32]runtime.NativeFunc, len(routes))
		for _, r := range routes {
			natives[r.methodID] = r.fn
		}
		if err := bound.BindSchemaNativeRoutes(schema, natives); err != nil {
			return nil, err
		}
		return bound, nil
	})
}
