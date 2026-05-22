package fmtlib

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type fakeFmtRegistrar struct {
	constants map[string]string
	routes    map[string]*runtime.RuntimeFuncSig
	templates *calltemplate.Registry
}

func newFakeFmtRegistrar() *fakeFmtRegistrar {
	return &fakeFmtRegistrar{
		constants: make(map[string]string),
		routes:    make(map[string]*runtime.RuntimeFuncSig),
		templates: calltemplate.NewRegistry(),
	}
}

func (f *fakeFmtRegistrar) RegisterFFISchema(name string, _ ffigo.FFIBridge, _ uint32, sig *runtime.RuntimeFuncSig, _ string) {
	f.routes[name] = sig
}

func (f *fakeFmtRegistrar) RegisterStructSchema(string, *runtime.RuntimeStructSpec) {}

func (f *fakeFmtRegistrar) RegisterInterfaceSchema(string, *runtime.RuntimeInterfaceSpec) {}

func (f *fakeFmtRegistrar) RegisterConstant(name, value string) {
	f.constants[name] = value
}

func (f *fakeFmtRegistrar) RegisterFunctionTemplate(tpl calltemplate.FunctionTemplate) error {
	return f.templates.Register(tpl)
}

func TestRegisterFmtAllRegistersPrintTemplatesWithFmt(t *testing.T) {
	executor := newFakeFmtRegistrar()

	RegisterFmtAll(executor, &FmtHost{}, ffigo.NewHandleRegistry())

	if executor.routes["fmt.Print"] == nil || executor.routes["fmt.Println"] == nil {
		t.Fatalf("expected fmt print routes, got %#v", executor.routes)
	}
	if _, ok := executor.constants["fmt.FMTKey"]; !ok {
		t.Fatalf("expected fmt constants, got %#v", executor.constants)
	}
	if tpl, ok := executor.templates.Global("print"); !ok || tpl.ID != "builtin.print" {
		t.Fatalf("expected builtin print template, got %#v ok=%v", tpl, ok)
	}
	if tpl, ok := executor.templates.Global("println"); !ok || tpl.ID != "builtin.println" {
		t.Fatalf("expected builtin println template, got %#v ok=%v", tpl, ok)
	}
}
