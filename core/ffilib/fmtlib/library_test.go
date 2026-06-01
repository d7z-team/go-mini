package fmtlib_test

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffilib/fmtlib"
	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
)

type outputRecorder struct {
	out strings.Builder
}

func (r *outputRecorder) Print(_ context.Context, s string) {
	r.out.WriteString(s)
}

func TestCoreFmtSourceLibrary(t *testing.T) {
	testutil.RunCases(t, nil, []testutil.Case{
		{
			Name:    "print-and-format",
			Imports: []string{"fmt"},
			Body: `
test.Out(fmt.Sprintf("%s-%d", "mini", 7))
test.Out("|")
test.Out(fmt.Sprint("v", Int64(1), Int64(2)))
`,
			Want: "mini-7|v1 2",
		},
		{
			Name:    "struct-and-pointer-formatting",
			Imports: []string{"fmt"},
			Decls: `
type User struct {
	Name string
	Age int64
}
`,
			Body: `
u := User{Name: "mini", Age: 7}
test.Out(fmt.Sprintf("%v|%v|%#v", u, &u, u))
`,
			Want: "{Name:mini Age:7}|&{Name:mini Age:7}|main.User{Name:mini Age:7}",
		},
		{
			Name:    "collections-and-bytes-formatting",
			Imports: []string{"fmt"},
			Body: `
test.Out(fmt.Sprintf("%v|%s|%x|%q", map[string]any{"b": 2, "a": []int64{1}}, []byte("go"), []byte("go"), "mini"))
`,
			Want: "map[a:[1] b:2]|go|676f|\"mini\"",
		},
		{
			Name:    "rune-formatting",
			Imports: []string{"fmt"},
			Body: `
test.Out(fmt.Sprintf("%q|%c|%q", 'A', 'A', '\n'))
`,
			Want: "'A'|A|'\\n'",
		},
		{
			Name:    "errorf-wraps-with-vm-formatting",
			Imports: []string{"errors", "fmt"},
			Body: `
base := errors.New("root")
other := errors.New("other")
wrapped := fmt.Errorf("outer %s: %w", "wrap", base)
multi := fmt.Errorf("multi: %w/%w", base, other)
test.Out(wrapped.Error())
test.Out("|")
test.OutBool(errors.Is(wrapped, base))
test.Out("|")
test.Out(errors.Unwrap(wrapped).Error())
test.Out("|")
test.OutBool(errors.Is(multi, base))
test.Out("|")
test.OutBool(errors.Is(multi, other))
test.Out("|")
test.OutBool(errors.Unwrap(multi) == nil)
`,
			Want: "outer wrap: root|true|root|true|true|true",
		},
	})
}

func TestCoreFmtPrintUsesVMFormatter(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

import "fmt"

type User struct {
	Name string
	Age int64
}

func main() {
	u := User{Name: "mini", Age: 7}
	fmt.Println("user", &u)
	fmt.Printf("%s:%d", "age", u.Age)
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	recorder := &outputRecorder{}
	if err := prog.Execute(fmtlib.WithOutputter(context.Background(), recorder)); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got, want := recorder.out.String(), "user &{Name:mini Age:7}\nage:7"; got != want {
		t.Fatalf("unexpected output %q, want %q", got, want)
	}
}
