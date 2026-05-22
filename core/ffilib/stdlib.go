package ffilib

import (
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/ffilib/errorslib"
	"gopkg.d7z.net/go-mini/core/ffilib/mathlib"
	"gopkg.d7z.net/go-mini/core/ffilib/sortlib"
	"gopkg.d7z.net/go-mini/core/ffilib/strconvlib"
	"gopkg.d7z.net/go-mini/core/ffilib/stringslib"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type Registrar interface {
	RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string)
	RegisterStructSchema(string, *runtime.RuntimeStructSpec)
	RegisterInterfaceSchema(string, *runtime.RuntimeInterfaceSpec)
	RegisterConstant(string, string)
	HandleRegistry() *ffigo.HandleRegistry
}

func RegisterAll(executor Registrar) {
	registry := executor.HandleRegistry()

	errorslib.RegisterErrors(executor, &errorslib.ErrorsHost{}, registry)
	executor.RegisterFFISchema(
		"errors.Is",
		&errorsIsBridge{registry: registry},
		methodIDErrorsIs,
		runtime.MustRuntimeFuncSig(runtime.SpecBool, false, runtime.SpecError, runtime.SpecError),
		"Check whether an error matches a target error",
	)
	stringslib.RegisterStrings(executor, &stringslib.StringsHost{}, registry)
	mathlib.RegisterMath(executor, &mathlib.MathHost{}, registry)
	strconvlib.RegisterStrconv(executor, &strconvlib.StrconvHost{}, registry)
	sortlib.RegisterSort(executor, &sortlib.SortHost{}, registry)
}
