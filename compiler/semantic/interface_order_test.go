package semantic

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/parser"
)

func TestInterfaceEmbeddingIsIndependentOfFileOrder(t *testing.T) {
	parsed := parser.ParseSource("example/order", "order.mgo", `package order
type Combined interface { Later }
type Later interface { Exit(code int) }
`)
	checked := Check(parsed.Program)
	if len(checked.Info.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %+v", checked.Info.Diagnostics)
	}
	object, ok := checked.Info.Lookup(checked.Info.PackageScope, "Combined")
	if !ok {
		t.Fatal("Combined type is missing")
	}
	methods, _, _, ok := checked.Info.Relations.View(object.Type).Interface()
	if !ok || len(methods) != 1 || methods[0].Name != "Exit" {
		t.Fatalf("Combined methods = %+v", methods)
	}
}
