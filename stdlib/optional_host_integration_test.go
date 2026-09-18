package stdlib_test

import (
	"context"
	"testing"

	"github.com/d7z-team/mini-go/ffi"
	"github.com/d7z-team/mini-go/rpc"
	rpcrouter "github.com/d7z-team/mini-go/rpc/router"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/stdlib/host/console"
	oshost "github.com/d7z-team/mini-go/stdlib/host/os"
)

func TestStandardLibraryRetriesMissingBinding(t *testing.T) {
	program := prepareStdlibRunProgram(t, "example/retry", `package retry
import (
 "fmt"
 "rpc"
)
func Run() string {
 _, err := fmt.Print("ready")
 code, _ := rpc.CodeOf(err)
 return code
}`)
	router := rpcrouter.New(rpcrouter.Options{})
	host, err := rpc.NewHost(rpc.HostOptions{Fallback: router})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: host})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Call(context.Background(), "run")
	if err != nil || len(result.Values) != 1 {
		t.Fatalf("first call = %#v, %v", result, err)
	}
	if code, _ := result.Values[0].StringValue(); code != "unimplemented" {
		t.Fatalf("first status = %q", code)
	}
	output := console.NewBuffer("")
	provider, err := console.NewFmtConsoleProvider(output)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := router.Publish([]rpcrouter.ProviderEntry{{Provider: provider}})
	if err != nil {
		t.Fatal(err)
	}
	defer publication.ForceClose(context.Background())
	result, err = instance.Call(context.Background(), "run")
	if err != nil || len(result.Values) != 1 {
		t.Fatalf("second call = %#v, %v", result, err)
	}
	if code, _ := result.Values[0].StringValue(); code != "" || output.Stdout() != "ready" {
		t.Fatalf("second status = %q, output=%q", code, output.Stdout())
	}
}

func TestStandardLibraryOptionalHost(t *testing.T) {
	program := prepareStdlibRunProgram(t, "example/optional", `package optional
import (
 "bytes"
 "errors"
 "fmt"
 "io/fs"
 "os"
 "rpc"
 "text/template"
 "time"
)
func Run(output, files bool) bool {
 var b bytes.Buffer
 parsed, err := template.New("test").Parse("{{.}}")
 if err != nil || parsed.Execute(&b, 42) != nil || b.String() != "42" { return false }
 if fmt.Sprintf("%d", 42) != "42" || !fs.ValidPath("a/b") || time.Unix(12, 0).Unix() != 12 { return false }
 n, err := fmt.Print("ok")
 if output { if err != nil || n != 2 { return false } } else {
  code, _ := rpc.CodeOf(err)
  if n != 0 || !errors.Is(err, errors.ErrUnsupported) || code != "unimplemented" { panic(fmt.Sprintf("print: %d %v %s", n, err, code)) }
 }
 // Failed binds are not cached; repeated calls retain the same public result.
 _, err = fmt.Print("")
 if output != (err == nil) { return false }
 file, err := os.Open("input.txt")
 if files {
  if err != nil { return false }
  if file.Close() != nil { return false }
 } else {
  var pathErr *os.PathError
  if !errors.As(err, &pathErr) || !errors.Is(err, errors.ErrUnsupported) { return false }
 }
 value, found := os.LookupEnv("KEY")
 return (!files && !found && value == "") || (files && found && value == "value")
}`)
	filesystem, err := oshost.NewMemoryFilesystem(map[string][]byte{"input.txt": []byte("input")})
	if err != nil {
		t.Fatal(err)
	}
	consoleProvider := func() (rpc.Provider, error) { return console.NewFmtConsoleProvider(console.NewBuffer("")) }
	for _, test := range []struct {
		name          string
		bridge        ffi.Bridge
		output, files bool
	}{
		{name: "no bridge"},
		{name: "empty host", bridge: newFFIBridge(t)},
		{name: "console only", bridge: newFFIBridge(t, consoleProvider), output: true},
		{name: "complete host", bridge: newFFIBridge(t, consoleProvider,
			func() (rpc.Provider, error) { return oshost.NewFilesystemProvider(filesystem) },
			func() (rpc.Provider, error) { return oshost.NewEnvironmentProvider(oshost.Map{"KEY": "value"}) },
		), output: true, files: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: test.bridge})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Call(context.Background(), "run", minigoruntime.HostBool(test.output), minigoruntime.HostBool(test.files))
			if err != nil || len(result.Values) != 1 {
				t.Fatalf("call = %#v, %v", result, err)
			}
			if passed, _ := result.Values[0].Bool(); !passed {
				t.Fatal("optional host behavior failed")
			}
		})
	}
}
