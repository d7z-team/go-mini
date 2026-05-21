package fmtlib

import (
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

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

func RegisterFmtAll(executor FmtRegistrar, impl Fmt, registry *ffigo.HandleRegistry) {
	RegisterFmt(executor, impl, registry)
	MustRegisterFmtTemplates(executor)
}

func MustRegisterFmtTemplates(registrar templateRegistrar) {
	if err := RegisterFmtTemplates(registrar); err != nil {
		panic(err)
	}
}

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
