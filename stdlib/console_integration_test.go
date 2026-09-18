package stdlib_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/rpc"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/stdlib/host/console"
)

type partialFailureReader struct{ delivered bool }

func (r *partialFailureReader) Read(buffer []byte) (int, error) {
	if r.delivered {
		return 0, io.EOF
	}
	r.delivered = true
	return copy(buffer, "1"), errors.New("read failed")
}

type deniedConsole struct{ *console.Buffer }

func (deniedConsole) Write(context.Context, int64, []uint8) (int64, console.FmtStreamFault, error) {
	return 0, console.FmtStreamFault{}, rpc.StatusError{Code: rpc.CodePermissionDenied, Message: "denied"}
}

func TestFmtPreservesHostFailures(t *testing.T) {
	program := prepareStdlibRunProgram(t, "example/consolefailure", `package consolefailure
import (
 "errors"
 "fmt"
 "rpc"
)
func Run() int {
 n, err := fmt.Print("output")
 code, _ := rpc.CodeOf(err)
 if n != 0 || code != "permission_denied" || errors.Is(err, errors.ErrUnsupported) { panic(fmt.Sprintf("Print: %d %v %s", n, err, code)) }
 var value int
 n, err = fmt.Scan(&value)
 if err != nil { panic(err) }
 return n
}`)
	output := deniedConsole{Buffer: console.NewBuffer("")}
	output.Stdin = &partialFailureReader{}
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: newFFIBridge(t,
		func() (rpc.Provider, error) { return console.NewFmtConsoleProvider(output) },
	)})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Call(context.Background(), "run")
	if err != nil || len(result.Values) != 1 {
		t.Fatalf("call = %#v, %v", result, err)
	}
	var nativeValue int
	want, nativeErr := fmt.Fscan(&partialFailureReader{}, &nativeValue)
	if count, _ := result.Values[0].Int64(); count != int64(want) || nativeErr != nil {
		t.Fatalf("Scan count = %d, Go = %d, %v", count, want, nativeErr)
	}
}

type failedOutputWriter struct{}

func (failedOutputWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type shortOutputWriter struct{}

func (shortOutputWriter) Write(data []byte) (int, error) { return len(data) / 2, nil }

func TestFmtPreservesPartialWrite(t *testing.T) {
	program := prepareStdlibRunProgram(t, "example/shortwrite", `package shortwrite
import (
 "errors"
 "fmt"
 "io"
)
func Run() bool {
 n, err := fmt.Print("four")
 return n == 2 && errors.Is(err, io.ErrShortWrite)
}`)
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: newFFIBridge(t,
		func() (rpc.Provider, error) {
			return console.NewFmtConsoleProvider(&console.Streams{Stdout: shortOutputWriter{}})
		},
	)})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Call(context.Background(), "run")
	if err != nil || len(result.Values) != 1 {
		t.Fatalf("call = %#v, %v", result, err)
	}
	if passed, _ := result.Values[0].Bool(); !passed {
		t.Fatal("partial write count or error lost")
	}
}

func TestPrintBuiltinsUseHostConsole(t *testing.T) {
	program := prepareStdlibProgram(t, "example/print", `package printtest
func Run() {
	print()
	print("a", 1)
	println("b", true)
}
`, []compiler.EntryPoint{{Name: "run", ModulePath: "example/print", Function: "Run"}})

	t.Run("output", func(t *testing.T) {
		output := console.NewBuffer("")
		instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: newFFIBridge(t,
			func() (rpc.Provider, error) { return console.NewFmtConsoleProvider(output) },
		)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := instance.Call(context.Background(), "run"); err != nil {
			t.Fatal(err)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
		if got := output.Stdout(); got != "a1b true\n" {
			t.Fatalf("stdout = %q", got)
		}
	})

	t.Run("write failure is discarded", func(t *testing.T) {
		output := &console.Streams{Stdout: failedOutputWriter{}}
		instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: newFFIBridge(t,
			func() (rpc.Provider, error) { return console.NewFmtConsoleProvider(output) },
		)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := instance.Call(context.Background(), "run"); err != nil {
			t.Fatalf("print exposed its discarded write error: %v", err)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestFmtScanUsesHostConsoleInput(t *testing.T) {
	program := prepareStdlibProgram(t, "example/scan", `package scantest
import "fmt"
func Run() int {
	var value int
	var word string
	n, err := fmt.Scan(&value, &word)
	if err != nil || n != 2 || word != "input" {
		return -1
	}
	return value
}
`, []compiler.EntryPoint{{Name: "run", ModulePath: "example/scan", Function: "Run"}})
	streams := console.NewBuffer("42 input\n")
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: newFFIBridge(t,
		func() (rpc.Provider, error) { return console.NewFmtConsoleProvider(streams) },
	)})
	if err != nil {
		t.Fatal(err)
	}
	result, callErr := instance.Call(context.Background(), "run")
	closeErr := instance.Close()
	if callErr != nil {
		t.Fatal(callErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if len(result.Values) != 1 {
		t.Fatalf("result = %#v", result.Values)
	}
	value, ok := result.Values[0].Int64()
	if !ok || value != 42 {
		t.Fatalf("result = %#v", result.Values[0])
	}
}
