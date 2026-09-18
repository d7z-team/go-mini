package randlib

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"math/rand"
	"sync"
	"time"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

var entropyRead = cryptorand.Read

type RandHost struct {
	mu sync.Mutex
	r  *rand.Rand
}

func NewRandHost() *RandHost {
	return &RandHost{r: newDefaultRand()}
}

func newDefaultRand() *rand.Rand {
	return rand.New(rand.NewSource(defaultSeed()))
}

func defaultSeed() int64 {
	var buf [8]byte
	if _, err := entropyRead(buf[:]); err == nil {
		return int64(binary.LittleEndian.Uint64(buf[:]))
	}
	return time.Now().UnixNano()
}

func withHostRand[T any](h *RandHost, fn func(*rand.Rand) T) T {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.r == nil {
		h.r = newDefaultRand()
	}
	return fn(h.r)
}

func (h *RandHost) ExpFloat64() float64 {
	return withHostRand(h, func(r *rand.Rand) float64 { return r.ExpFloat64() })
}

func (h *RandHost) Float32() float32 {
	return withHostRand(h, func(r *rand.Rand) float32 { return r.Float32() })
}

func (h *RandHost) Float64() float64 {
	return withHostRand(h, func(r *rand.Rand) float64 { return r.Float64() })
}
func (h *RandHost) Int() int { return withHostRand(h, func(r *rand.Rand) int { return r.Int() }) }
func (h *RandHost) Int31() int32 {
	return withHostRand(h, func(r *rand.Rand) int32 { return r.Int31() })
}

func (h *RandHost) Int31n(n int32) int32 {
	return withHostRand(h, func(r *rand.Rand) int32 { return r.Int31n(n) })
}

func (h *RandHost) Intn(n int) int {
	return withHostRand(h, func(r *rand.Rand) int { return r.Intn(n) })
}

func (h *RandHost) Int63() int64 {
	return withHostRand(h, func(r *rand.Rand) int64 { return r.Int63() })
}

func (h *RandHost) Int63n(n int64) int64 {
	return withHostRand(h, func(r *rand.Rand) int64 { return r.Int63n(n) })
}

func (h *RandHost) NormFloat64() float64 {
	return withHostRand(h, func(r *rand.Rand) float64 { return r.NormFloat64() })
}

func (h *RandHost) Seed(seed int64) {
	withHostRand(h, func(r *rand.Rand) struct{} {
		r.Seed(seed)
		return struct{}{}
	})
}

func (h *RandHost) Perm(n int) []int {
	return withHostRand(h, func(r *rand.Rand) []int { return r.Perm(n) })
}

func (h *RandHost) Uint32() uint32 {
	return withHostRand(h, func(r *rand.Rand) uint32 { return r.Uint32() })
}

func (h *RandHost) Uint64() uint64 {
	return withHostRand(h, func(r *rand.Rand) uint64 { return r.Uint64() })
}

func (h *RandHost) Read(p *ffigo.BytesRef) (int, error) {
	if p == nil {
		return 0, nil
	}
	return entropyRead(p.Value)
}
