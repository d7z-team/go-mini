package fmtlib

import (
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// FmtRegistrar is the executor surface needed to register the fmt package and
// its compiler-only print templates.
type FmtRegistrar interface {
	RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string)
	RegisterStructSchema(string, *runtime.RuntimeStructSpec)
	RegisterInterfaceSchema(string, *runtime.RuntimeInterfaceSpec)
	RegisterConstant(string, string)
	RegisterFunctionTemplate(calltemplate.FunctionTemplate) error
}

type templateRegistrar interface {
	RegisterFunctionTemplate(calltemplate.FunctionTemplate) error
}

// RegisterFmtAll registers the fmt runtime package and the compiler-only
// print/println templates.
func RegisterFmtAll(executor FmtRegistrar, impl Fmt, registry *ffigo.HandleRegistry) {
	RegisterFmt(executor, impl, registry)
	MustRegisterFmtTemplates(executor)
}

// MustRegisterFmtTemplates registers fmt call templates and panics on invalid
// built-in template definitions.
func MustRegisterFmtTemplates(registrar templateRegistrar) {
	if err := RegisterFmtTemplates(registrar); err != nil {
		panic(err)
	}
}

// RegisterFmtTemplates registers compiler-only templates for print and println.
func RegisterFmtTemplates(registrar templateRegistrar) error {
	for _, tpl := range fmtTemplates() {
		if err := registrar.RegisterFunctionTemplate(tpl); err != nil {
			return err
		}
	}
	return nil
}

func fmtTemplates() []calltemplate.FunctionTemplate {
	return []calltemplate.FunctionTemplate{
		{
			ID:        "builtin.print",
			Name:      "print",
			SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
			Body:      `{{ pkg "fmt" }}.Print({{ args }})`,
		},
		{
			ID:        "builtin.println",
			Name:      "println",
			SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
			Body:      `{{ pkg "fmt" }}.Println({{ args }})`,
		},
	}
}
