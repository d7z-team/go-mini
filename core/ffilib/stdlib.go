package ffilib

import (
	"gopkg.d7z.net/go-mini/core/ffilib/errorslib"
	"gopkg.d7z.net/go-mini/core/ffilib/mathlib"
	"gopkg.d7z.net/go-mini/core/ffilib/sortlib"
	"gopkg.d7z.net/go-mini/core/ffilib/strconvlib"
	"gopkg.d7z.net/go-mini/core/ffilib/stringslib"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

func Surface() *surface.Bundle {
	return surface.Merge(
		errorslib.SurfaceErrors(&errorslib.ErrorsHost{}),
		errorsIsSurface(),
		stringslib.SurfaceStrings(&stringslib.StringsHost{}),
		mathlib.SurfaceMath(&mathlib.MathHost{}),
		strconvlib.SurfaceStrconv(&strconvlib.StrconvHost{}),
		sortlib.SurfaceSort(&sortlib.SortHost{}),
	)
}

func errorsIsSurface() *surface.Bundle {
	const name = "errors.Is"
	sig := runtime.MustRuntimeFuncSig(runtime.SpecBool, false, runtime.SpecError, runtime.SpecError)
	doc := "Check whether an error matches a target error"
	schema := runtime.NewFFISurfaceSchema()
	schema.AddFunc("errors", "Is", name, methodIDErrorsIs, sig, doc)
	return surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		bound := runtime.NewBoundFFISurface(schema)
		bound.AddRoute("errors", "Is", runtime.FFIRoute{
			Name:     name,
			Bridge:   &errorsIsBridge{registry: ctx.Registry},
			MethodID: methodIDErrorsIs,
			FuncSig:  sig,
			Doc:      doc,
		})
		return bound, nil
	})
}
