package rpc

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodeOfPreservesWrappedStatusForms(t *testing.T) {
	tests := []error{
		fmt.Errorf("wrapped: %w", StatusError{Code: CodeAborted, Message: "value"}),
		fmt.Errorf("wrapped: %w", &StatusError{Code: CodeDataLoss, Message: "pointer"}),
	}
	wants := []struct {
		code    Code
		message string
	}{{CodeAborted, "value"}, {CodeDataLoss, "pointer"}}
	for index, err := range tests {
		code, message := CodeOf(err)
		if code != wants[index].code || message != wants[index].message {
			t.Fatalf("CodeOf(%v) = %q, %q", err, code, message)
		}
	}
}

func TestUnsupportedStatus(t *testing.T) {
	for _, code := range []Code{CodeUnimplemented, CodeFailedPrecondition, CodeUnavailable, CodePermissionDenied, CodeCanceled} {
		status := StatusError{Code: code, Message: "failure"}
		for _, err := range []error{status, &status, fmt.Errorf("operation: %w", status)} {
			if got := errors.Is(err, errors.ErrUnsupported); got != (code == CodeUnimplemented) {
				t.Fatalf("unsupported category for %v = %v", err, got)
			}
			if got, _ := CodeOf(err); got != code {
				t.Fatalf("CodeOf(%v) = %v", err, got)
			}
		}
	}
}
