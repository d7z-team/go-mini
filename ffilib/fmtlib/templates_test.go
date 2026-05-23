package fmtlib

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/calltemplate"
)

func TestSurfaceIncludesPrintTemplatesWithFmt(t *testing.T) {
	bundle := Surface(&FmtHost{})
	if bundle == nil || bundle.Schema == nil {
		t.Fatal("expected fmt surface schema")
	}
	pkg := bundle.Schema.Packages["fmt"]
	if pkg == nil {
		t.Fatalf("expected fmt package schema, got %#v", bundle.Schema.Packages)
	}
	if pkg.Members["Print"] == nil || pkg.Members["Println"] == nil {
		t.Fatalf("expected fmt print routes, got %#v", pkg.Members)
	}
	if pkg.Members["FMTKey"] == nil {
		t.Fatalf("expected fmt constants, got %#v", pkg.Members)
	}

	templates := calltemplate.NewRegistry()
	for _, tpl := range bundle.Templates {
		if err := templates.Register(tpl); err != nil {
			t.Fatal(err)
		}
	}
	if tpl, ok := templates.Global("print"); !ok || tpl.ID != "builtin.print" {
		t.Fatalf("expected builtin print template, got %#v ok=%v", tpl, ok)
	}
	if tpl, ok := templates.Global("println"); !ok || tpl.ID != "builtin.println" {
		t.Fatalf("expected builtin println template, got %#v ok=%v", tpl, ok)
	}
}
