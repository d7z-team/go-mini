package ffigo

import (
	"fmt"
)

// ResultStatus represents the outcome of an FFI call.
type ResultStatus byte

const (
	StatusSuccess ResultStatus = 0
	StatusError   ResultStatus = 1
)

// WrapError converts a Go error into a format suitable for the FFI Tuple protocol.
// It ensures that even nil errors are handled (though nil should usually use StatusSuccess).
func WrapError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Errorf is a helper for creating formatted error strings for FFI.
func Errorf(format string, a ...interface{}) string {
	return fmt.Sprintf(format, a...)
}
