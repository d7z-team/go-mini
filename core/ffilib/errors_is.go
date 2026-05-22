package ffilib

import (
	"context"
	goerrors "errors"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

const methodIDErrorsIs uint32 = 1

type errorsIsBridge struct {
	registry *ffigo.HandleRegistry
}

func (b *errorsIsBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req == nil {
		return nil, goerrors.New("errors.Is: missing FFI request")
	}
	if req.MethodID != methodIDErrorsIs {
		return nil, fmt.Errorf("errors.Is: unknown method ID %d", req.MethodID)
	}
	return b.call(req.Args), nil
}

func (b *errorsIsBridge) Invoke(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req == nil {
		return nil, goerrors.New("errors.Is: missing FFI request")
	}
	if req.Method != "" && req.Method != "Is" && req.Method != "errors.Is" {
		return nil, fmt.Errorf("errors.Is: unknown method %s", req.Method)
	}
	return b.call(req.Args), nil
}

func (b *errorsIsBridge) DestroyHandle(handle uint32) error {
	if b.registry != nil {
		b.registry.Remove(handle)
	}
	return nil
}

func (b *errorsIsBridge) call(args []byte) []byte {
	reader := ffigo.NewReader(args)
	errData := reader.ReadRawError()
	targetData := reader.ReadRawError()

	match := errData.Handle != 0 && errData.Handle == targetData.Handle
	if !match {
		match = goerrors.Is(b.errorFromData(errData), b.errorFromData(targetData))
	}

	buf := ffigo.GetBuffer()
	buf.WriteBool(match)
	out := append([]byte(nil), buf.Bytes()...)
	ffigo.ReleaseBuffer(buf)
	return out
}

func (b *errorsIsBridge) errorFromData(data ffigo.ErrorData) error {
	if data.Message == "" && data.Handle == 0 {
		return nil
	}
	if data.Handle != 0 && b.registry != nil {
		if obj, ok := b.registry.Get(data.Handle); ok {
			if err, ok := obj.(error); ok {
				return err
			}
			return wireError{message: fmt.Sprint(obj)}
		}
	}
	return wireError{message: data.Message}
}

type wireError struct {
	message string
	_       []byte
}

func (e wireError) Error() string {
	return e.message
}
