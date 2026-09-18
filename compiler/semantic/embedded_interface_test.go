package semantic

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/parser"
)

func TestEmbeddedMethodsSatisfyInterfaceRelations(t *testing.T) {
	parsed := parser.ParseSource("example/embedded", "embedded.mgo", `package embedded
type Position int
func (Position) position() int { return 0 }
type Node interface { position() int }
type Value struct { Position }
func Use(node Node) {}
func Test(value *Value) { Use(value) }
`)
	checked := Check(parsed.Program)
	if len(checked.Info.Diagnostics) != 0 {
		t.Fatalf("embedded method relation diagnostics: %#v", checked.Info.Diagnostics)
	}
}
