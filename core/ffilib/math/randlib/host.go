package randlib

import (
	"math/rand"
)

type RandHost struct{}

func (h *RandHost) Float64() float64       { return rand.Float64() }
func (h *RandHost) Int() int               { return rand.Int() }
func (h *RandHost) Intn(n int) int         { return rand.Intn(n) }
func (h *RandHost) Int63() int64           { return rand.Int63() }
func (h *RandHost) Int63n(n int64) int64   { return rand.Int63n(n) }
func (h *RandHost) Seed(seed int64)        { rand.Seed(seed) }
func (h *RandHost) Perm(n int) []int       { return rand.Perm(n) }
