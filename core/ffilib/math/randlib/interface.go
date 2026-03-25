//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg randlib -path gopkg.d7z.net/go-mini/core/ffilib/math/randlib -out rand_ffigen.go interface.go
package randlib

// Rand 接口定义了随机数生成操作

// ffigen:module math/rand
type Rand interface {
	Float64() float64
	Int() int
	Intn(n int) int
	Int63() int64
	Int63n(n int64) int64
	Seed(seed int64)
	Perm(n int) []int
}
