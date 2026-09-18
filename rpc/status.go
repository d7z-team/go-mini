package rpc

import (
	"context"
	"errors"
)

type Code string

const (
	CodeCanceled           Code = "canceled"
	CodeUnknown            Code = "unknown"
	CodeDeadlineExceeded   Code = "deadline_exceeded"
	CodeInvalidArgument    Code = "invalid_argument"
	CodeNotFound           Code = "not_found"
	CodeAlreadyExists      Code = "already_exists"
	CodePermissionDenied   Code = "permission_denied"
	CodeResourceExhausted  Code = "resource_exhausted"
	CodeFailedPrecondition Code = "failed_precondition"
	CodeAborted            Code = "aborted"
	CodeOutOfRange         Code = "out_of_range"
	CodeUnimplemented      Code = "unimplemented"
	CodeInternal           Code = "internal"
	CodeUnavailable        Code = "unavailable"
	CodeDataLoss           Code = "data_loss"
	CodeUnauthenticated    Code = "unauthenticated"
	CodeProtocol           Code = "protocol"
)

type StatusError struct {
	Code    Code
	Message string
}

// Is reports whether the status describes an unsupported operation.
func (e StatusError) Is(target error) bool {
	return e.Code == CodeUnimplemented && target == errors.ErrUnsupported
}

func CodeOf(err error) (Code, string) {
	if err == nil {
		return "", ""
	}
	status := statusError(err)
	if typed, ok := asStatusError(status); ok {
		return typed.Code, typed.Message
	}
	return CodeInternal, status.Error()
}

func (e StatusError) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

func statusError(err error) error {
	if err == nil {
		return nil
	}
	if status, ok := asStatusError(err); ok {
		return status
	}
	if errors.Is(err, context.Canceled) {
		return StatusError{Code: CodeCanceled, Message: err.Error()}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return StatusError{Code: CodeDeadlineExceeded, Message: err.Error()}
	}
	return StatusError{Code: CodeInternal, Message: err.Error()}
}

func asStatusError(err error) (StatusError, bool) {
	var value StatusError
	if errors.As(err, &value) {
		return value, true
	}
	var pointer *StatusError
	if errors.As(err, &pointer) && pointer != nil {
		return *pointer, true
	}
	return StatusError{}, false
}
