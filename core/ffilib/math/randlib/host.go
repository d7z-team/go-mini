package randlib

import (
	"math/rand"
	"sync"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type RandHost struct{}

var sharedRand = struct {
	mu sync.Mutex
	r  *rand.Rand
}{
	r: rand.New(rand.NewSource(1)),
}

func withRand[T any](fn func(*rand.Rand) T) T {
	sharedRand.mu.Lock()
	defer sharedRand.mu.Unlock()
	return fn(sharedRand.r)
}

func (h *RandHost) ExpFloat64() float64 {
	return withRand(func(r *rand.Rand) float64 { return r.ExpFloat64() })
}

func (h *RandHost) Float32() float32 {
	return withRand(func(r *rand.Rand) float32 { return r.Float32() })
}

func (h *RandHost) Float64() float64 {
	return withRand(func(r *rand.Rand) float64 { return r.Float64() })
}
func (h *RandHost) Int() int     { return withRand(func(r *rand.Rand) int { return r.Int() }) }
func (h *RandHost) Int31() int32 { return withRand(func(r *rand.Rand) int32 { return r.Int31() }) }
func (h *RandHost) Int31n(n int32) int32 {
	return withRand(func(r *rand.Rand) int32 { return r.Int31n(n) })
}
func (h *RandHost) Intn(n int) int { return withRand(func(r *rand.Rand) int { return r.Intn(n) }) }
func (h *RandHost) Int63() int64   { return withRand(func(r *rand.Rand) int64 { return r.Int63() }) }
func (h *RandHost) Int63n(n int64) int64 {
	return withRand(func(r *rand.Rand) int64 { return r.Int63n(n) })
}

func (h *RandHost) NormFloat64() float64 {
	return withRand(func(r *rand.Rand) float64 { return r.NormFloat64() })
}

func (h *RandHost) Seed(seed int64) {
	withRand(func(r *rand.Rand) struct{} {
		r.Seed(seed)
		return struct{}{}
	})
}
func (h *RandHost) Perm(n int) []int { return withRand(func(r *rand.Rand) []int { return r.Perm(n) }) }
func (h *RandHost) Uint32() uint32   { return withRand(func(r *rand.Rand) uint32 { return r.Uint32() }) }
func (h *RandHost) Uint64() uint64   { return withRand(func(r *rand.Rand) uint64 { return r.Uint64() }) }
func (h *RandHost) Read(p *ffigo.BytesRef) (int, error) {
	if p == nil {
		return 0, nil
	}
	type readResult struct {
		n   int
		err error
	}
	res := withRand(func(r *rand.Rand) readResult {
		n, err := r.Read(p.Value)
		return readResult{n: n, err: err}
	})
	return res.n, res.err
}
