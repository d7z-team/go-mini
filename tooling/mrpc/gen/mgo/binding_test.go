package mgo_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	minigo "github.com/d7z-team/mini-go"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/rpc"
	"github.com/d7z-team/mini-go/rpc/router"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/tooling/mrpc"
	"github.com/d7z-team/mini-go/tooling/mrpc/gen/mgo"
)

type bindingCounter struct {
	rpc.Provider
	binds atomic.Int32
	first rpc.Code
}

func (p *bindingCounter) BindRPC(ctx context.Context, request rpc.BindRequest) (rpc.ProviderLease, error) {
	if p.binds.Add(1) == 1 && p.first != "" {
		if p.first == rpc.CodeDeadlineExceeded {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, rpc.StatusError{Code: p.first, Message: "binding rejected"}
	}
	return p.Provider.BindRPC(ctx, request)
}

func TestGeneratedClientBindingAndRevisionOwnership(t *testing.T) {
	file, diagnostics := mrpc.Parse(mrpc.Source{Path: "service.mrpc", Text: `syntax = "mrpc/v2";
namespace example.binding;
option mgo_package = "example/binding";
service Service { Value() returns (value string = 1); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	catalog, err := mrpc.NewCatalog([]mrpc.File{file})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := mgo.Generate(catalog, mgo.Options{Package: "binding"})
	if err != nil {
		t.Fatal(err)
	}
	const script = `package binding
import "rpc"
var retained *ServiceClient
func Try() string {
    client, err := BindServiceClient(rpc.ClientOptions{TimeoutNanoseconds: 100000000})
    if err != nil {
        if client != nil { return "partial client" }
        code, _ := rpc.CodeOf(err)
        if code == "unimplemented" { return "local" }
        return code
    }
    defer client.Close()
    value, err := client.Value()
    if err != nil { return err.Error() }
    return value
}
func Concurrent() string {
    client := rpc.NewClient(ServiceContract(), rpc.ClientOptions{})
    done := make(chan error, 4)
    for i := 0; i < 4; i++ { go func() { done <- client.Bind() }() }
    for i := 0; i < 4; i++ { if err := <-done; err != nil { return err.Error() } }
    if err := client.Close(); err != nil { return err.Error() }
    if client.Bind() == nil { return "reopened" }
    return "closed"
}
func Retry() string {
    client := rpc.NewClient(ServiceContract(), rpc.ClientOptions{})
    defer client.Close()
    if err := client.Bind(); err == nil { return "expected initial failure" }
    if err := client.Bind(); err != nil { return err.Error() }
    return "retried"
}
func Retain() string {
    var err error
    if retained == nil { retained, err = BindServiceClient(rpc.ClientOptions{}) }
    if err != nil { return err.Error() }
    value, err := retained.Value()
    if err != nil { return err.Error() }
    return value + "-before"
}
func Release() string {
    if err := retained.Close(); err != nil { return err.Error() }
    retained = nil
    return "released"
}
`
	cacheRoot, err := cache.ResolveDiskRoot("")
	if err != nil {
		t.Fatal(err)
	}
	compile := func(text string, symbols bool) *minigoruntime.Program {
		t.Helper()
		sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "example/binding", Files: []source.File{
			{Path: "binding_gen.mgo", Text: string(generated)}, {Path: "main.mgo", Text: text},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		engine, err := minigo.New(minigo.Config{Sources: sources, Cache: cache.NewDiskBackend(cacheRoot), Symbols: symbols})
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Close()
		program, result, err := engine.Compile("example/binding", minigo.EntryPoint{Name: "try", Function: "Try"}, minigo.EntryPoint{Name: "concurrent", Function: "Concurrent"}, minigo.EntryPoint{Name: "retry", Function: "Retry"}, minigo.EntryPoint{Name: "retain", Function: "Retain"}, minigo.EntryPoint{Name: "release", Function: "Release"})
		if err != nil || !result.OK() {
			t.Fatalf("compile: %v, %#v", err, result.Diagnostics)
		}
		return program
	}
	program := compile(script, true)
	call := func(t *testing.T, instance *minigoruntime.Instance, entry, want string) {
		t.Helper()
		result, err := instance.Call(context.Background(), entry)
		if err != nil || len(result.Values) != 1 {
			t.Fatalf("call %s: %#v, %v", entry, result, err)
		}
		if got, ok := result.Values[0].StringValue(); !ok || got != want {
			t.Fatalf("%s = %q, want %q", entry, got, want)
		}
	}
	method := rpc.Method{ID: "example.binding::Service.Value", Service: "example.binding::Service", Name: "Value", ContractHash: catalog.ContractID}
	for _, test := range []struct {
		name, entry, want string
		first             rpc.Code
		binds             int32
	}{
		{"empty", "try", "local", "", 0},
		{"eager", "try", "host", "", 1},
		{"concurrent", "concurrent", "closed", "", 1},
		{"retry", "retry", "retried", rpc.CodeUnimplemented, 2},
		{"permission", "try", "permission_denied", rpc.CodePermissionDenied, 1},
		{"timeout", "try", "deadline_exceeded", rpc.CodeDeadlineExceeded, 1},
		{"mismatch", "try", "failed_precondition", "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			declared := method
			if test.name == "mismatch" {
				declared.ContractHash = strings.Repeat("b", 64)
			}
			var calls atomic.Int32
			provider, err := rpc.NewProvider(rpc.MethodBinding{Method: declared, Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) {
				calls.Add(1)
				return []rpc.Value{{Type: "string", Data: "host"}}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			counter := &bindingCounter{Provider: provider, first: test.first}
			options := rpc.HostOptions{Providers: []rpc.Provider{counter}}
			if test.name == "empty" {
				options.Providers = nil
			}
			host, err := rpc.NewHost(options)
			if err != nil {
				t.Fatal(err)
			}
			defer host.Close()
			instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: host})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			call(t, instance, test.entry, test.want)
			if counter.binds.Load() != test.binds {
				t.Fatalf("binds=%d want=%d", counter.binds.Load(), test.binds)
			}
			wantCalls := int32(0)
			if test.name == "eager" {
				wantCalls = 1
			}
			if calls.Load() != wantCalls {
				t.Fatalf("business calls=%d want=%d", calls.Load(), wantCalls)
			}
		})
	}

	routes := router.New(router.Options{})
	defer routes.ForceShutdown(context.Background())
	register := func(value string) *router.Registration {
		t.Helper()
		provider, err := rpc.NewProvider(rpc.MethodBinding{Method: method, Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) {
			return []rpc.Value{{Type: "string", Data: value}}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		registration, err := routes.Register(provider, router.RegistrationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return registration
	}
	first := register("first")
	host, err := rpc.NewHost(rpc.HostOptions{Fallback: routes})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: host})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	call(t, instance, "retain", "first-before")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := first.Close(canceled); err == nil {
		t.Fatal("close wait should be canceled while the old route is retained")
	}
	register("second")
	patched := compile(strings.ReplaceAll(script, "-before", "-after"), false)
	plan, err := instance.PreparePatch(context.Background(), patched)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.ApplyPatch(plan); err != nil {
		t.Fatal(err)
	}
	call(t, instance, "retain", "first-after")
	call(t, instance, "try", "second")
	call(t, instance, "release", "released")
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := first.Status(); status.ActiveLeases != 0 || status.PendingBinds != 0 {
		t.Fatalf("old provider retained state: %#v", status)
	}
	// Rebuilding with a populated cache must remain independent of registration.
	warm := compile(strings.ReplaceAll(script, "-before", "-after"), false)
	if warm.Hash() != patched.Hash() {
		t.Fatal("provider registration changed the cached program identity")
	}
	call(t, instance, "retain", "second-after")
	call(t, instance, "release", "released")
}
