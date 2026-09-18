package mgo_test

import (
	"context"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/rpc"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
	stdlibcore "github.com/d7z-team/mini-go/stdlib"
	"github.com/d7z-team/mini-go/tooling/mrpc"
	mgo "github.com/d7z-team/mini-go/tooling/mrpc/gen/mgo"
)

func TestGenerateProducesOrdinaryCompilableMiniGo(t *testing.T) {
	file, diagnostics := mrpc.Parse(mrpc.Source{Path: "greeter.mrpc", Text: `syntax = "mrpc/v2";
namespace example.greeter.v1;
option mgo_package = "example/greeter";
message Header { Name string = 1; }
message Request { Name string = 1; Headers []Header = 2; }
message Response { Message string = 1; }
service Greeter { Hello(request Request = 1) returns (response Response = 1); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatalf("parse: %#v", diagnostics)
	}
	catalog, err := mrpc.NewCatalog([]mrpc.File{file})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := mgo.Generate(catalog, mgo.Options{Package: "greeter"})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"type Request struct", "type GreeterClient struct", "type GreeterHandler interface", "func ServeGreeter", "func NewGreeterClient", catalog.ContractID,
		`Type: "[]example.greeter.v1.Header"`,
	} {
		if !strings.Contains(string(generated), expected) {
			t.Fatalf("generated source misses %q:\n%s", expected, generated)
		}
	}
	application, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/greeter",
		Files: []source.File{
			{Path: "greeter_mrpc_gen.mgo", Text: string(generated)},
			{Path: "main.mgo", Text: `package greeter
import (
	"rpc"
)
func Main() string {
	client := NewGreeterClient(rpc.ClientOptions{})
	defer client.Close()
	response, err := client.Hello(Request{Name: "MiniGo"})
	if err != nil { return err.Error() }
	return response.Message
}
func Timeout() string {
	client := NewGreeterClient(rpc.ClientOptions{TimeoutNanoseconds: 1000000})
	defer client.Close()
	_, err := client.Hello(Request{Name: "timeout"})
	if err == nil { return "missing timeout" }
	return err.Error()
}
	`},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	library := stdlibcore.Open()
	standardSources, err := workspace.StandardLibrary(library)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := workspace.MergeSourceSets(application, standardSources)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := compiler.Check(compiler.Request{Root: "example/greeter", Sources: sources})
	if err != nil || !checked.OK() {
		t.Fatalf("check generated source: err=%v diagnostics=%#v\n%s", err, checked.Diagnostics, generated)
	}
	prepared, err := compiler.Prepare(compiler.Request{Root: "example/greeter", Sources: sources, EntryPoints: []compiler.EntryPoint{
		{Name: "main", Function: "Main"}, {Name: "timeout", Function: "Timeout"},
	}})
	if err != nil || !prepared.Checked.OK() || prepared.Image == nil {
		t.Fatalf("prepare generated source: err=%v diagnostics=%#v", err, prepared.Checked.Diagnostics)
	}
	method := rpc.Method{ID: "example.greeter.v1::Greeter.Hello", Service: "example.greeter.v1::Greeter", Name: "Hello", ContractHash: catalog.ContractID}
	provider, err := rpc.NewProvider(rpc.MethodBinding{Method: method, Invoke: func(ctx context.Context, arguments []rpc.Value) ([]rpc.Value, error) {
		if len(arguments) != 1 || arguments[0].Type != "example.greeter.v1.Request" {
			t.Fatalf("arguments = %#v", arguments)
		}
		fields, _ := arguments[0].Data.([]rpc.Field)
		if len(fields) != 0 && fields[0].Value.Data == "timeout" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return []rpc.Value{{Type: "example.greeter.v1.Response", Data: []rpc.Field{{ID: 1, Value: rpc.Value{Type: "string", Data: "Hello, MiniGo"}}}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	binder, err := rpc.NewLocalBinder(rpc.LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	host, err := rpc.NewHost(rpc.HostOptions{Fallback: binder})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	program, err := minigoruntime.LoadExecutionImage(*prepared.Image)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: host})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Call(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	value, ok := result.Values[0].StringValue()
	if !ok || value != "Hello, MiniGo" {
		t.Fatalf("result = %#v", result.Values)
	}
	timed, err := instance.Call(context.Background(), "timeout")
	if err != nil {
		t.Fatal(err)
	}
	timeoutText, ok := timed.Values[0].StringValue()
	if !ok || !strings.HasPrefix(timeoutText, "deadline_exceeded:") {
		t.Fatalf("timeout result = %#v", timed.Values)
	}
}

func TestGenerateResourceFloatingPointImportsMath(t *testing.T) {
	file, diagnostics := mrpc.Parse(mrpc.Source{Path: "stream.mrpc", Text: `syntax = "mrpc/v2";
namespace example.stream.v1;
option mgo_package = "example/stream";
resource Stream { Scale(value float64 = 1) returns (result complex128 = 1); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	catalog, err := mrpc.NewCatalog([]mrpc.File{file})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := mgo.Generate(catalog, mgo.Options{Package: "stream"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `"math"`) {
		t.Fatalf("generated resource codec misses math import:\n%s", generated)
	}
	application, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/stream", Files: []source.File{{Path: "stream_mrpc_gen.mgo", Text: string(generated)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	standardSources, err := workspace.StandardLibrary(stdlibcore.Open())
	if err != nil {
		t.Fatal(err)
	}
	sources, err := workspace.MergeSourceSets(application, standardSources)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := compiler.Check(compiler.Request{Root: "example/stream", Sources: sources})
	if err != nil || !checked.OK() {
		t.Fatalf("check generated source: err=%v diagnostics=%#v\n%s", err, checked.Diagnostics, generated)
	}
}

func TestGenerateRejectsMutatedCatalogIdentity(t *testing.T) {
	file, diagnostics := mrpc.Parse(mrpc.Source{Path: "service.mrpc", Text: `syntax = "mrpc/v2";
namespace example.service.v1;
option mgo_package = "example/service";
service Service { Ping() returns (); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	catalog, err := mrpc.NewCatalog([]mrpc.File{file})
	if err != nil {
		t.Fatal(err)
	}
	catalog.ContractID = "mutated"
	if _, err := mgo.Generate(catalog, mgo.Options{Package: "service"}); err == nil {
		t.Fatal("Generate accepted a catalog with a stale identity")
	}
}
