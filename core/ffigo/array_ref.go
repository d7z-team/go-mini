package ffigo

// ArrayRef marks a []T parameter as inout across the FFI boundary.
// The host may replace Value with a different slice length, and the final
// value will be copied back to the caller when the bridge call returns.
type ArrayRef[T any] struct {
	Value []T
}
