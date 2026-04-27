//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg randlib -out rand_ffigen.go interface.go
package randlib

import "gopkg.d7z.net/go-mini/core/ffigo"

// Rand 接口定义了随机数生成操作

// ffigen:module math/rand
type Rand interface {
	ExpFloat64() float64
	Float32() float32
	Float64() float64
	Int() int
	Int31() int32
	Int31n(n int32) int32
	Intn(n int) int
	Int63() int64
	Int63n(n int64) int64
	NormFloat64() float64
	Read(p *ffigo.BytesRef) (int, error)
	Seed(seed int64)
	Perm(n int) []int
	Uint32() uint32
	Uint64() uint64
}
