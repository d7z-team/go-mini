package ffigo

import (
	"fmt"
)

// WrapError converts a Go error into a format suitable for the FFI Tuple protocol.
// It ensures that even nil errors are handled.
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
