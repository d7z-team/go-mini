package gogen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/d7z-team/mini-go/tooling/mrpc"
)

func TestImportedEnumAndResourceBindingsCompile(t *testing.T) {
	dependencyFile, diagnostics := mrpc.Parse(mrpc.Source{Path: "model.mrpc", Text: `syntax = "mrpc/v2";
namespace example.model.v1;
option go_package = "example/model;model";
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
option go_package = "example/service;service";
import model "example/model.mrpc";
service Service { Open(result model.State = 1) returns (values model.File = 1); Use(ctx model.File = 1) returns (err model.State = 1); Fail(file model.File = 1) returns (existing model.File = 1, fresh model.File = 2, state model.State = 3); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	root, err := mrpc.NewCatalogWithDependencies([]mrpc.File{rootFile}, map[string]mrpc.Catalog{"example/model.mrpc": dependency})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	repo, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	mod := "module example\n\ngo 1.26\n\nrequire github.com/d7z-team/mini-go v0.0.0\nreplace github.com/d7z-team/mini-go => " + filepath.ToSlash(repo) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, catalog := range map[string]mrpc.Catalog{"model": dependency, "service": root} {
		generated, err := Generate(catalog, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "binding.go"), generated, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const lifecycle = `package service_test
import (
 "context"
 "io"
 "sync"
 "testing"
 "example/model"
 "example/service"
 "github.com/d7z-team/mini-go/rpc"
 "github.com/d7z-team/mini-go/rpc/router"
)
type file struct { closed int; state model.State }
func TestImportedEnumBoundsAndUnknownValues(t *testing.T) {
 for _,n:=range []int32{-2147483648,37,2147483647} {
  value,err:=model.EncodeState(model.State(n));if err!=nil{t.Fatal(err)}
  got,err:=model.DecodeState(value);if err!=nil || int32(got)!=n{t.Fatalf("enum roundtrip: %v %v",got,err)}
 }
 for _,n:=range []int64{-2147483649,2147483648} {
  value,err:=model.EncodeState(model.State(0));if err!=nil{t.Fatal(err)};value.Data=n
  if _,err:=model.DecodeState(value);err==nil{t.Fatal("enum overflow accepted")}
 }
}
func (f *file) State(context.Context) (model.State,error) { return f.state,nil }
func (f *file) Close(context.Context) error { f.closed++; return nil }
type handler struct { file *file }
func (h handler) Open(ctx context.Context, state model.State) (model.FileHandler,error) { h.file.state=state; return h.file,nil }
func (h handler) Use(ctx context.Context, value model.FileHandler) (model.State,error) { return value.State(ctx) }
func (h handler) Fail(context.Context, model.FileHandler) (model.FileHandler,model.FileHandler,model.State,error) { panic("fault injection must intercept Fail") }
type malformedProvider struct { rpc.Provider; fresh *file }
func (p malformedProvider) BindRPC(ctx context.Context, request rpc.BindRequest) (rpc.ProviderLease,error) { lease,err:=p.Provider.BindRPC(ctx,request);if err!=nil{return nil,err};return malformedLease{lease,p.fresh},nil }
type malformedLease struct { rpc.ProviderLease; fresh *file }
func (p malformedLease) Invoke(ctx context.Context, method rpc.Method, arguments []rpc.Value) (*rpc.ProviderResult,error) {
 if method.Name!="Fail" { return p.ProviderLease.Invoke(ctx,method,arguments) }
 fresh,err:=model.ExportFile(ctx,p.fresh);if err!=nil{return nil,err}
 return &rpc.ProviderResult{Values:[]rpc.Value{arguments[0],fresh,{Type:"bool",Data:true}}},nil
}
type messagePipe struct { incoming, outgoing chan []byte; done chan struct{}; once *sync.Once }
func (p *messagePipe) Read(ctx context.Context) ([]byte,error) { select { case data:=<-p.incoming: return data,nil; case <-p.done: return nil,io.EOF; case <-ctx.Done(): return nil,ctx.Err() } }
func (p *messagePipe) Write(ctx context.Context, data []byte) error { data=append([]byte(nil),data...); select { case p.outgoing<-data: return nil; case <-p.done: return io.ErrClosedPipe; case <-ctx.Done(): return ctx.Err() } }
func (p *messagePipe) Close() error { p.once.Do(func(){close(p.done)});return nil }
func TestImportedResourceLifecycle(t *testing.T) {
 ctx:=context.Background(); resource,fresh:=&file{},&file{}
 provider,err:=service.NewServiceProvider(handler{resource}); if err!=nil { t.Fatal(err) }
 routes:=router.New(router.Options{}); defer routes.Shutdown(ctx)
 if _,err:=routes.Register(malformedProvider{provider,fresh},router.RegistrationOptions{});err!=nil { t.Fatal(err) }
 a,b,done,once:=make(chan []byte,16),make(chan []byte,16),make(chan struct{}),new(sync.Once)
 left,err:=rpc.OpenEndpoint(&messagePipe{a,b,done,once},rpc.EndpointServices{Binder:routes},rpc.EndpointOptions{});if err!=nil {t.Fatal(err)};defer left.Close()
 right,err:=rpc.OpenEndpoint(&messagePipe{b,a,done,once},rpc.EndpointServices{Binder:routes},rpc.EndpointOptions{});if err!=nil {t.Fatal(err)};defer right.Close()
 for index,binding:=range []rpc.Binder{routes,left,right} {
 client,err:=service.BindServiceClient(ctx,binding,rpc.BindOptions{});if err!=nil { t.Fatal(err) };defer client.Close()
 value,err:=client.Open(ctx,model.State(1));if err!=nil { t.Fatal(err) }
 state,err:=value.State(ctx);if err!=nil || state!=1 { t.Fatalf("resource call: %v %v",state,err) }
 state,err=client.Use(ctx,value);if err!=nil || state!=1 { t.Fatalf("resource argument: %v %v",state,err) }
 previous,created,wrong,err:=client.Fail(ctx,value)
 if err==nil || previous!=nil || created!=nil || wrong!=0 {t.Fatalf("partial results published: %v %v %v %v",previous,created,wrong,err)}
 if fresh.closed!=index+1 {t.Fatalf("discarded resource close count: %d",fresh.closed)}
 if state,err:=value.State(ctx);err!=nil || state!=1 {t.Fatalf("discard released existing reference: %v %v",state,err)}
 other,err:=service.BindServiceClient(ctx,binding,rpc.BindOptions{});if err!=nil { t.Fatal(err) };defer other.Close()
 if _,err:=other.Use(ctx,value);err==nil { t.Fatal("foreign handle accepted") }
 copyValue:=*value
 if err:=value.Close(ctx);err!=nil { t.Fatal(err) }
 if _,err:=copyValue.State(ctx);err==nil { t.Fatal("closed copy remained usable") }
 if err:=copyValue.Close(ctx);err!=nil || resource.closed!=index+1 { t.Fatalf("close count: %d %v",resource.closed,err) }
 }
}
`
	if err := os.WriteFile(filepath.Join(dir, "service", "binding_test.go"), []byte(lifecycle), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "-race", "-mod=mod", "./...")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated packages: %v\n%s", err, output)
	}
}
