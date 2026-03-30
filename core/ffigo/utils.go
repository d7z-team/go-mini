package ffigo

import (
	"fmt"
	"time"
)

// ToConstantString converts a host-side value to a string that go-mini can use
func ToConstantString(v interface{}) string {
	if d, ok := v.(time.Duration); ok {
		return fmt.Sprint(int64(d))
	}
	return fmt.Sprint(v)
}
