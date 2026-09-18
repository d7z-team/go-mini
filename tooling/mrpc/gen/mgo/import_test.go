package mgo_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/rpc"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/stdlib"

	"github.com/d7z-team/mini-go/tooling/mrpc"
	mgo "github.com/d7z-team/mini-go/tooling/mrpc/gen/mgo"
)

func TestGenerateImportsLanguagePackageAndUsesContractNamespace(t *testing.T) {
	dependencyFile, diagnostics := mrpc.Parse(mrpc.Source{Path: "model.mrpc", Text: `syntax = "mrpc/v2";
namespace example.model.v1;
option mgo_package = "example/model";
message Model { Name string = 1; }
enum State { Unknown = 0; Ready = 1; }
resource File { State() returns (state State = 1); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	dependency, err := mrpc.NewCatalog([]mrpc.File{dependencyFile})
	if err != nil {
		t.Fatal(err)
	}
	rootFile, diagnostics := mrpc.Parse(mrpc.Source{Path: "service.mrpc", Text: `syntax = "mrpc/v2";
namespace example.service.v1;
option mgo_package = "example/service";
import model "example/model.mrpc";
service Service { Get(request model.Model = 1) returns (); }
service Files { Open(state model.State = 1) returns (file model.File = 1); Use(file model.File = 1) returns (state model.State = 1); Fail(file model.File = 1) returns (existing model.File = 1, fresh model.File = 2, state model.State = 3); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	catalog, err := mrpc.NewCatalogWithDependencies([]mrpc.File{rootFile}, map[string]mrpc.Catalog{"example/model.mrpc": dependency})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := mgo.Generate(catalog, mgo.Options{Package: "service"})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`model "example/model"`, "model.Model", "model.EncodeModel(arg0)"} {
		if !strings.Contains(string(generated), expected) {
			t.Fatalf("generated source misses %q:\n%s", expected, generated)
		}
	}
	modelSource, err := mgo.Generate(dependency, mgo.Options{Package: "model"})
	if err != nil {
		t.Fatal(err)
	}
	application, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "example/model", Files: []source.File{{Path: "model.mgo", Text: string(modelSource)}}},
		{ModulePath: "example/service", Files: []source.File{{Path: "service.mgo", Text: string(generated)}, {Path: "main.mgo", Text: `package service
import ("rpc"; "example/model")
func Main() string {
	for _, number := range []int32{-2147483648,37,2147483647} {
		decoded, err := model.DecodeState(model.EncodeState(model.State(number)))
		if err != nil || int32(decoded) != number { return "enum roundtrip failed" }
	}
	for _, number := range []int64{-2147483649,2147483648} {
		value := model.EncodeState(model.State(0)); value.Signed = number
		if _,err := model.DecodeState(value); err == nil { return "enum overflow accepted" }
	}
	client := NewFilesClient(rpc.ClientOptions{})
	defer client.Close()
	file, err := client.Open(model.State(2147483647))
	if err != nil { return err.Error() }
	state, err := file.State()
	if err != nil || state != model.State(2147483647) { return "resource method failed" }
	state, err = client.Use(file)
	if err != nil || state != model.State(2147483647) { return "resource parameter failed" }
	existing, fresh, wrongState, err := client.Fail(file)
	if err == nil || wrongState != 0 { return "partial typed result was published" }
	if _, err := existing.State(); err == nil { return "partial existing handle was published" }
	if _, err := fresh.State(); err == nil { return "partial new handle was published" }
	if _, err := file.State(); err != nil { return "discard closed the pre-existing resource" }
	copyFile := file
	if err := file.Close(); err != nil { return err.Error() }
	if _, err := copyFile.State(); err == nil { return "closed alias survived" }
	if err := copyFile.Close(); err != nil { return err.Error() }
	return "ok"
}`}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	standard, err := workspace.StandardLibrary(stdlib.Open())
	if err != nil {
		t.Fatal(err)
	}
	sources, err := workspace.MergeSourceSets(application, standard)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := compiler.Check(compiler.Request{Root: "example/service", Sources: sources})
	if err != nil || !checked.OK() {
		t.Fatalf("imported bindings: %v\n%#v", err, checked.Diagnostics)
	}
	prepared, err := compiler.Prepare(compiler.Request{Root: "example/service", Sources: sources, EntryPoints: []compiler.EntryPoint{{Name: "main", Function: "Main"}}})
	if err != nil || !prepared.Checked.OK() || prepared.Image == nil {
		t.Fatalf("prepare: %v %+v", err, prepared.Checked.Diagnostics)
	}
	resource := &importedTestResource{}
	failedResource := &importedTestResource{}
	resourceHash := dependency.ResourceTypeID("File")
	open := rpc.Method{ID: "example.service.v1::Files.Open", Service: "example.service.v1::Files", Name: "Open", ContractHash: catalog.ContractID}
	use := rpc.Method{ID: "example.service.v1::Files.Use", Service: "example.service.v1::Files", Name: "Use", ContractHash: catalog.ContractID}
	state := rpc.Method{ID: "example.model.v1::File.State", Service: "example.model.v1::File", Name: "State", ContractHash: dependency.ContractID, ResourceTypeHash: resourceHash}
	fail := rpc.Method{ID: "example.service.v1::Files.Fail", Service: "example.service.v1::Files", Name: "Fail", ContractHash: catalog.ContractID}
	provider, err := rpc.NewProvider(
		rpc.MethodBinding{Method: fail, Invoke: func(ctx context.Context, arguments []rpc.Value) ([]rpc.Value, error) {
			fresh, err := rpc.Export(ctx, failedResource, resourceHash)
			return []rpc.Value{arguments[0], fresh, {Type: "bool", Data: true}}, err
		}},
		rpc.MethodBinding{Method: open, Invoke: func(ctx context.Context, arguments []rpc.Value) ([]rpc.Value, error) {
			resource.state = arguments[0]
			value, err := rpc.Export(ctx, resource, resourceHash)
			return []rpc.Value{value}, err
		}},
		rpc.MethodBinding{Method: use, Invoke: func(ctx context.Context, arguments []rpc.Value) ([]rpc.Value, error) {
			value, err := rpc.Resolve(ctx, *arguments[0].Resource)
			if err != nil {
				return nil, err
			}
			return value.Invoke(ctx, state.Name, nil)
		}},
		rpc.MethodBinding{Method: state},
	)
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
	if text, ok := result.Values[0].StringValue(); !ok || text != "ok" {
		t.Fatalf("imported resource execution: %q", text)
	}
	if resource.closes.Load() != 1 {
		t.Fatalf("resource closed %d times", resource.closes.Load())
	}
	if failedResource.closes.Load() != 1 {
		t.Fatalf("discarded resource closed %d times", failedResource.closes.Load())
	}
}

type importedTestResource struct {
	state  rpc.Value
	closes atomic.Int32
}

func (r *importedTestResource) Invoke(context.Context, string, []rpc.Value) ([]rpc.Value, error) {
	return []rpc.Value{r.state}, nil
}
func (r *importedTestResource) Close(context.Context) error { r.closes.Add(1); return nil }
