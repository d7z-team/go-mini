package ffigo

import (
	"fmt"
	"strconv"
	"time"
)

// ToConstantString converts a host-side value to a string that go-mini can use
func ToConstantString(v interface{}) string {
	if d, ok := v.(time.Duration); ok {
		return strconv.FormatInt(int64(d), 10)
	}
	return fmt.Sprint(v)
}
