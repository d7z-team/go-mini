//go:generate go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg sortlib -out sort_ffigen.go interface.go
package sortlib

import "gopkg.d7z.net/go-mini/core/ffigo"

// Sort 接口定义了排序操作

// ffigen:module sort
type Sort interface {
	Ints(x *ffigo.ArrayRef[int64])
	Float64s(x *ffigo.ArrayRef[float64])
	Strings(x *ffigo.ArrayRef[string])
	IntsAreSorted(x []int64) bool
	Float64sAreSorted(x []float64) bool
	StringsAreSorted(x []string) bool
}
