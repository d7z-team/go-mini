package fmtlib

import (
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

func Surface(impl Fmt) *surface.Bundle {
	return surface.Merge(
		SurfaceFmt(impl),
		surface.Templates(fmtTemplates()...),
	)
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
