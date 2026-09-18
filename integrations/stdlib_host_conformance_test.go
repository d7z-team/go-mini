package integrations

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/rpc"
	"github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/runtime/bytecode"
	stdhost "github.com/d7z-team/mini-go/stdlib/host"
	"github.com/d7z-team/mini-go/stdlib/host/console"
	oshost "github.com/d7z-team/mini-go/stdlib/host/os"
)

func TestSharedStandardLibraryHostScenarios(t *testing.T) {
	var scenario struct {
		Version     int
		Environment struct {
			Entries []string
			Lookups []struct {
				Key, Value string
				Found      bool
			}
		}
		Filesystem []struct {
			ID, Operation, Data, Code      string
			Size, Offset, Whence, Position int64
			Nil                            bool
		}
		VM struct {
			Environment []string
			Result      int64
			Stdout      string
		}
	}
	data, err := os.ReadFile("../testdata/stdlib-host/scenarios.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &scenario); err != nil {
		t.Fatal(err)
	}
	if scenario.Version != 1 {
		t.Fatal("unknown host corpus version")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	filesystem, err := oshost.NewMemoryFilesystem(map[string][]byte{"input": []byte("abc")})
	if err != nil {
		t.Fatal(err)
	}
	filesystemProvider, err := oshost.NewFilesystemProvider(filesystem)
	if err != nil {
		t.Fatal(err)
	}
	environmentProvider, err := oshost.NewEnvironmentProvider(oshost.Snapshot(scenario.Environment.Entries))
	if err != nil {
		t.Fatal(err)
	}
	binder, err := rpc.NewLocalBinder(rpc.LocalBinderOptions{}, filesystemProvider, environmentProvider)
	if err != nil {
		t.Fatal(err)
	}
	client, err := oshost.BindOsFilesystemClient(ctx, binder, rpc.BindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	file, fault, err := client.Open(ctx, "input", 2, 0)
	if err != nil || fault.Code != "" {
		t.Fatalf("open: %v %v", fault, err)
	}
	defer file.Close(ctx)
	for _, action := range scenario.Filesystem {
		t.Run(action.ID, func(t *testing.T) {
			var data []byte
			var position int64
			var fault oshost.OsOperationFault
			var err error
			switch action.Operation {
			case "read":
				data, fault, err = file.Read(ctx, action.Size)
			case "read_at":
				data, fault, err = file.ReadAt(ctx, action.Size, action.Offset)
			case "write_at":
				position, fault, err = file.WriteAt(ctx, []byte(action.Data), action.Offset)
			case "seek":
				position, fault, err = file.Seek(ctx, action.Offset, action.Whence)
			default:
				t.Fatalf("unknown operation %s", action.Operation)
			}
			if err != nil || fault.Code != action.Code {
				t.Fatalf("fault: %v %v", fault, err)
			}
			if action.Operation == "read" || action.Operation == "read_at" {
				if string(data) != action.Data || (data == nil) != action.Nil {
					t.Fatalf("data: %#v", data)
				}
			} else if position != action.Position {
				t.Fatalf("position: %d", position)
			}
		})
	}
	environment, err := oshost.BindOsEnvironmentClient(ctx, binder, rpc.BindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Close()
	for _, lookup := range scenario.Environment.Lookups {
		value, found, err := environment.Lookup(ctx, lookup.Key)
		if err != nil || value != lookup.Value || found != lookup.Found {
			t.Fatalf("lookup %s: %q %v %v", lookup.Key, value, found, err)
		}
	}
	t.Run("precompiled-vm", func(t *testing.T) {
		source, err := os.Open("../testdata/stdlib-host/images/call.json.gz")
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		compressed, err := gzip.NewReader(source)
		if err != nil {
			t.Fatal(err)
		}
		defer compressed.Close()
		var image bytecode.ExecutionImage
		if err := json.NewDecoder(compressed).Decode(&image); err != nil {
			t.Fatal(err)
		}
		program, err := runtime.LoadExecutionImage(image)
		if err != nil {
			t.Fatal(err)
		}
		output := console.NewBuffer("")
		consoleProvider, err := console.NewFmtConsoleProvider(output)
		if err != nil {
			t.Fatal(err)
		}
		environmentProvider, err := oshost.NewEnvironmentProvider(oshost.Snapshot(scenario.VM.Environment))
		if err != nil {
			t.Fatal(err)
		}
		host, err := stdhost.New(stdhost.Options{Required: []string{"console", "environment", "filesystem"}, Providers: []stdhost.Provider{
			{Capability: "console", RPC: consoleProvider}, {Capability: "environment", RPC: environmentProvider}, {Capability: "filesystem", RPC: filesystemProvider},
		}})
		if err != nil {
			t.Fatal(err)
		}
		defer host.Close()
		instance, err := program.Instantiate(ctx, runtime.InstanceOptions{FFI: host})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		result, err := instance.Call(ctx, "default")
		if err != nil {
			t.Fatal(err)
		}
		value, ok := result.Values[0].Int64()
		if !ok || value != scenario.VM.Result || output.Stdout() != scenario.VM.Stdout {
			t.Fatalf("VM: %v stdout=%q", result.Values, output.Stdout())
		}
	})
}
