//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg sortlib -module sort -path gopkg.d7z.net/go-mini/core/ffilib/sortlib -out sort_ffigen.go interface.go
package sortlib

// Sort 接口定义了排序操作

// ffigen:module sort
type Sort interface {
	Ints(x []int64) []int64
	Float64s(x []float64) []float64
	Strings(x []string) []string
	IntsAreSorted(x []int64) bool
	Float64sAreSorted(x []float64) bool
	StringsAreSorted(x []string) bool
}
