package ffilib

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestErrorsIsBridgeUsesGoErrorTree(t *testing.T) {
	registry := ffigo.NewHandleRegistry()
	target := errors.New("target")
	wrapped := fmt.Errorf("wrapped: %w", target)

	if !callErrorsIsBridge(t, registry, wrapped, target) {
		t.Fatal("expected wrapped error to match target")
	}
	if callErrorsIsBridge(t, registry, errors.New("target"), target) {
		t.Fatal("different errors with the same text should not match")
	}
	if !callErrorsIsBridge(t, registry, nil, nil) {
		t.Fatal("nil should match nil")
	}
}

func callErrorsIsBridge(t *testing.T, registry *ffigo.HandleRegistry, errValue, targetValue error) bool {
	t.Helper()

	buf := ffigo.GetBuffer()
	writeErrorArg := func(err error) {
		if err == nil {
			buf.WriteRawError("", 0)
			return
		}
		buf.WriteRawError(err.Error(), registry.Register(err))
	}
	writeErrorArg(errValue)
	writeErrorArg(targetValue)

	bridge := &errorsIsBridge{registry: registry}
	ret, err := bridge.Call(context.Background(), &ffigo.FFICallRequest{
		MethodID: methodIDErrorsIs,
		Args:     append([]byte(nil), buf.Bytes()...),
	})
	ffigo.ReleaseBuffer(buf)
	if err != nil {
		t.Fatalf("errors.Is bridge failed: %v", err)
	}
	retData, err := ffigo.SyncBytes(ret)
	if err != nil {
		t.Fatalf("errors.Is bridge returned invalid payload: %v", err)
	}
	return ffigo.NewReader(retData).ReadBool()
}
