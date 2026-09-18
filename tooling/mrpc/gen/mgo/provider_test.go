package mgo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/rpc"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
	stdlibcore "github.com/d7z-team/mini-go/stdlib"
	"github.com/d7z-team/mini-go/tooling/mrpc"
	mgo "github.com/d7z-team/mini-go/tooling/mrpc/gen/mgo"
)

func TestGeneratedMiniGoProviderServesHostCalls(t *testing.T) {
	modelFile, diagnostics := mrpc.Parse(mrpc.Source{Path: "counter.mrpc", Text: `syntax = "mrpc/v2";
namespace example.counter.v1;
option mgo_package = "example/counter";
enum Step { Zero = 0; }
resource Counter { Next() returns (value Step = 1); Fork() returns (counter Counter = 1); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	model, err := mrpc.NewCatalog([]mrpc.File{modelFile})
	if err != nil {
		t.Fatal(err)
	}
	modelSource, err := mgo.Generate(model, mgo.Options{Package: "counter"})
	if err != nil {
		t.Fatal(err)
	}
	file, diagnostics := mrpc.Parse(mrpc.Source{Path: "greeter.mrpc", Text: `syntax = "mrpc/v2";
namespace example.greeter.v1;
option mgo_package = "example/greeter";
import model "example/counter.mrpc";
message Request { Name string = 1; }
message Response { Message string = 1; }
service Greeter {
	Hello(request Request = 1) returns (response Response = 1);
	Open() returns (counter model.Counter = 1);
	Use(counter model.Counter = 1) returns (value model.Step = 1);
}
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatalf("parse: %#v", diagnostics)
	}
	catalog, err := mrpc.NewCatalogWithDependencies([]mrpc.File{file}, map[string]mrpc.Catalog{"example/counter.mrpc": model})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := mgo.Generate(catalog, mgo.Options{Package: "greeter"})
	if err != nil {
		t.Fatal(err)
	}
	application, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "example/counter", Files: []source.File{{Path: "counter.mgo", Text: string(modelSource)}}}, {
			ModulePath: "example/greeter",
			Files: []source.File{
				{Path: "greeter_mrpc_gen.mgo", Text: string(generated)},
				{Path: "serve.mgo", Text: `package greeter
import "rpc"
import model "example/counter"
type greeterHandler struct{}
type counter struct { value int64 }
var handlerState string
func (greeterHandler) Hello(context *rpc.Context, request Request) (Response, error) {
	if request.Name == "panic" { panic("handler panic") }
	if request.Name == "wait" {
		handlerState = "waiting"
		<-context.Done()
		if context.Err() != nil { handlerState = "canceled" }
		return Response{}, context.Err()
	}
	if request.Name == "status" {
		return Response{Message: handlerState}, nil
	}
	return Response{Message: "Hello, " + request.Name}, nil
}
func (greeterHandler) Open(context *rpc.Context) (model.CounterHandler, error) { return &counter{}, nil }
func (greeterHandler) Use(context *rpc.Context, value model.CounterHandler) (model.Step, error) { return value.Next(context) }
func (c *counter) Next(context *rpc.Context) (model.Step, error) { c.value++; return model.Step(c.value), nil }
func (c *counter) Fork(context *rpc.Context) (model.CounterHandler,error) { return &counter{value:c.value},nil }
func Serve() error { return ServeGreeter(greeterHandler{}, rpc.ServerOptions{Name: "guest"}) }
`},
			},
		},
	})
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
	prepared, err := compiler.Prepare(compiler.Request{Root: "example/greeter", Sources: sources, EntryPoints: []compiler.EntryPoint{{Name: "serve", Function: "Serve"}}})
	if err != nil || !prepared.Checked.OK() || prepared.Image == nil {
		t.Fatalf("prepare generated provider: err=%v diagnostics=%#v\n%s", err, prepared.Checked.Diagnostics, generated)
	}
	published := make(chan rpc.Provider, 1)
	unpublished := make(chan struct{}, 1)
	host, err := rpc.NewHost(rpc.HostOptions{PublishProvider: func(_ context.Context, provider rpc.Provider) (func() error, error) {
		published <- provider
		return func() error { unpublished <- struct{}{}; return nil }, nil
	}})
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
	serveCtx, cancelServe := context.WithCancel(context.Background())
	defer instance.Close()
	defer cancelServe()
	serveResult := make(chan error, 1)
	go func() {
		_, callErr := instance.Call(serveCtx, "serve")
		serveResult <- callErr
	}()
	var provider rpc.Provider
	select {
	case provider = <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("generated provider was not published")
	}
	binder, err := rpc.NewLocalBinder(rpc.LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	method := rpc.Method{ID: "example.greeter.v1::Greeter.Hello", Service: "example.greeter.v1::Greeter", Name: "Hello", ContractHash: catalog.ContractID}
	routes, err := binder.Bind(context.Background(), rpc.BindRequest{Contract: provider.RPCContract()})
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Close()
	result, err := routes.Call(context.Background(), rpc.Call{Method: method, Arguments: []rpc.Value{{Type: "example.greeter.v1.Request", Data: []rpc.Field{{ID: 1, Value: rpc.Value{Type: "string", Data: "VM"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	fields, ok := result.Values[0].Data.([]rpc.Field)
	if !ok || len(fields) != 1 || fields[0].Value.Data != "Hello, VM" {
		t.Fatalf("result = %#v", result.Values)
	}
	if err := result.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancelCall := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() {
		_, callErr := routes.Call(cancelCtx, rpc.Call{Method: method, Arguments: []rpc.Value{{Type: "example.greeter.v1.Request", Data: []rpc.Field{{ID: 1, Value: rpc.Value{Type: "string", Data: "wait"}}}}}})
		canceled <- callErr
	}()
	readHandlerState := func() string {
		status, callErr := routes.Call(context.Background(), rpc.Call{Method: method, Arguments: []rpc.Value{{Type: "example.greeter.v1.Request", Data: []rpc.Field{{ID: 1, Value: rpc.Value{Type: "string", Data: "status"}}}}}})
		if callErr != nil {
			t.Fatal(callErr)
		}
		if len(status.Values) != 1 {
			t.Fatalf("handler status = %#v", status.Values)
		}
		fields, _ := status.Values[0].Data.([]rpc.Field)
		if len(fields) != 1 {
			t.Fatalf("handler status = %#v", status.Values)
		}
		if err := status.Accept(context.Background()); err != nil {
			t.Fatal(err)
		}
		value, _ := fields[0].Value.Data.(string)
		return value
	}
	waitForHandlerState := func(want string) {
		deadline := time.Now().Add(5 * time.Second)
		for {
			if got := readHandlerState(); got == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("handler did not reach state %q", want)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitForHandlerState("waiting")
	cancelCall()
	cancelErr := <-canceled
	cancelCode, _ := rpc.CodeOf(cancelErr)
	if !errors.Is(cancelErr, context.Canceled) && cancelCode != rpc.CodeCanceled {
		t.Fatalf("canceled provider call = %v", cancelErr)
	}
	waitForHandlerState("canceled")
	_, err = routes.Call(context.Background(), rpc.Call{Method: method, Arguments: []rpc.Value{{Type: "example.greeter.v1.Request", Data: []rpc.Field{{ID: 1, Value: rpc.Value{Type: "string", Data: "panic"}}}}}})
	if code, _ := rpc.CodeOf(err); code != rpc.CodeInternal {
		t.Fatalf("handler panic error = %v", err)
	}
	openMethod := rpc.Method{ID: "example.greeter.v1::Greeter.Open", Service: "example.greeter.v1::Greeter", Name: "Open", ContractHash: catalog.ContractID}
	opened, err := routes.Call(context.Background(), rpc.Call{Method: openMethod})
	if err != nil {
		t.Fatal(err)
	}
	if len(opened.Values) != 1 || opened.Values[0].Resource == nil {
		t.Fatalf("open result = %#v", opened.Values)
	}
	resource := *opened.Values[0].Resource
	if err := opened.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	nextMethod := rpc.Method{ID: "example.counter.v1::Counter.Next", Service: "example.counter.v1::Counter", Name: "Next", ContractHash: model.ContractID, ResourceTypeHash: model.ResourceTypeID("Counter")}
	next, err := routes.Call(context.Background(), rpc.Call{Method: nextMethod, Receiver: &resource})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Values) != 1 || next.Values[0].Data != int64(1) {
		t.Fatalf("next result = %#v", next.Values)
	}
	if err := next.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	useMethod := rpc.Method{ID: "example.greeter.v1::Greeter.Use", Service: "example.greeter.v1::Greeter", Name: "Use", ContractHash: catalog.ContractID}
	used, err := routes.Call(context.Background(), rpc.Call{Method: useMethod, Arguments: []rpc.Value{opened.Values[0]}})
	if err != nil {
		t.Fatal(err)
	}
	if len(used.Values) != 1 || used.Values[0].Type != "example.counter.v1.Step" || used.Values[0].Data != int64(2) {
		t.Fatalf("imported resource parameter: %+v", used.Values)
	}
	if err := used.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	forkMethod := rpc.Method{ID: "example.counter.v1::Counter.Fork", Service: "example.counter.v1::Counter", Name: "Fork", ContractHash: model.ContractID, ResourceTypeHash: model.ResourceTypeID("Counter")}
	forked, err := routes.Call(context.Background(), rpc.Call{Method: forkMethod, Receiver: &resource})
	if err != nil {
		t.Fatal(err)
	}
	if len(forked.Values) != 1 || forked.Values[0].Resource == nil {
		t.Fatalf("resource method result: %+v", forked.Values)
	}
	if err := forked.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := routes.Drop(context.Background(), *forked.Values[0].Resource); err != nil {
		t.Fatal(err)
	}
	if err := routes.Drop(context.Background(), resource); err != nil {
		t.Fatal(err)
	}
	if err := routes.Close(); err != nil {
		t.Fatal(err)
	}
	cancelServe()
	select {
	case <-serveResult:
	case <-time.After(5 * time.Second):
		t.Fatal("generated provider did not stop")
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-unpublished:
	case <-time.After(5 * time.Second):
		t.Fatal("generated provider was not unpublished")
	}
}
