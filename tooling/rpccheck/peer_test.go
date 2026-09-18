package rpccheck

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/d7z-team/mini-go/rpc"
	service "github.com/d7z-team/mini-go/testdata/rpc/generated/go/service"
)

func TestTypedLaboratoryContract(t *testing.T) {
	handler := &Laboratory{}
	provider, err := service.NewLaboratoryProvider(handler)
	if err != nil {
		t.Fatal(err)
	}
	binder, err := rpc.NewLocalBinder(rpc.LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Exercise(context.Background(), binder)
	if err != nil {
		t.Fatal(err)
	}
	if report != (Report{Echo: 3, Recursive: true, Resource: 42, Canceled: true}) {
		t.Fatalf("report: %+v", report)
	}
	if handler.Released.Load() != 1 {
		t.Fatalf("resource closed %d times", handler.Released.Load())
	}
}

func TestSharedResourceScenarios(t *testing.T) {
	data, err := os.ReadFile("../../testdata/rpc/scenarios/resources.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenarios []struct {
		Name     string
		Actions  []string
		Released []int64
	}
	if err := json.Unmarshal(data, &scenarios); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			handler := &Laboratory{}
			provider, err := service.NewLaboratoryProvider(handler)
			if err != nil {
				t.Fatal(err)
			}
			binder, err := rpc.NewLocalBinder(rpc.LocalBinderOptions{}, provider)
			if err != nil {
				t.Fatal(err)
			}
			routes, err := binder.Bind(context.Background(), rpc.BindRequest{Contract: provider.RPCContract()})
			if err != nil {
				t.Fatal(err)
			}
			defer routes.Close()
			var method rpc.Method
			for _, candidate := range provider.RPCContract().Methods {
				if candidate.Name == "Open" {
					method = candidate
				}
			}
			var result *rpc.Result
			var reference rpc.ResourceRef
			for index, action := range scenario.Actions {
				switch action {
				case "open":
					result, err = routes.Call(context.Background(), rpc.Call{Method: method, Arguments: []rpc.Value{{Type: "int64", Data: int64(40)}}})
					if err == nil {
						reference = *result.Values[0].Resource
					}
				case "discard":
					err = result.Discard(context.Background())
				case "accept":
					err = result.Accept(context.Background())
				case "release":
					err = routes.Drop(context.Background(), reference)
				case "wrong_epoch":
					wrong := reference
					wrong.Epoch++
					code, _ := rpc.CodeOf(routes.Drop(context.Background(), wrong))
					if code != rpc.CodeInvalidArgument {
						t.Fatalf("wrong epoch: %s", code)
					}
				case "wrong_owner":
					handle, bindErr := routes.BindResource(reference)
					if bindErr != nil {
						t.Fatal(bindErr)
					}
					other, bindErr := binder.Bind(context.Background(), rpc.BindRequest{Contract: provider.RPCContract()})
					if bindErr != nil {
						t.Fatal(bindErr)
					}
					_, referenceErr := handle.Reference(context.Background(), other)
					code, _ := rpc.CodeOf(referenceErr)
					if closeErr := other.Shutdown(context.Background()); closeErr != nil {
						t.Fatal(closeErr)
					}
					if code != rpc.CodeInvalidArgument {
						t.Fatalf("wrong owner: %s", code)
					}
				case "alias_release":
					first, bindErr := routes.BindResource(reference)
					if bindErr != nil {
						t.Fatal(bindErr)
					}
					second, bindErr := routes.BindResource(reference)
					if bindErr != nil {
						t.Fatal(bindErr)
					}
					if err = first.Close(context.Background()); err == nil {
						err = second.Close(context.Background())
					}
				case "close":
					err = routes.Shutdown(context.Background())
				default:
					t.Fatalf("unknown action %q", action)
				}
				if err != nil {
					t.Fatalf("%s: %v", action, err)
				}
				if got := handler.Released.Load(); got != scenario.Released[index] {
					t.Fatalf("after %s: released %d, want %d", action, got, scenario.Released[index])
				}
			}
		})
	}
}
