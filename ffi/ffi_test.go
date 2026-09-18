package ffi

import (
	"context"
	"testing"
)

func TestFunctionAdapters(t *testing.T) {
	canceled := false
	bridge := CallFunc(func(_ context.Context, request Request, complete Completion) (Call, error) {
		complete(Result{Payload: append([]byte(nil), request.Payload...)})
		return CancelFunc(func() { canceled = true }), nil
	})
	session, err := bridge.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Shutdown(context.Background())
	var result Result
	call, err := session.Start(context.Background(), Request{Route: "echo", Payload: []byte("value")}, func(value Result) { result = value })
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if string(result.Payload) != "value" {
		t.Fatalf("payload = %q", result.Payload)
	}
	call.Cancel()
	if !canceled {
		t.Fatal("Cancel did not invoke adapter")
	}
}
