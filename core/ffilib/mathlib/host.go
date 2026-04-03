package mathlib

import (
	"math"
)

type MathHost struct{}

func (h *MathHost) Abs(x float64) float64          { return math.Abs(x) }
func (h *MathHost) Ceil(x float64) float64         { return math.Ceil(x) }
func (h *MathHost) Floor(x float64) float64        { return math.Floor(x) }
func (h *MathHost) Round(x float64) float64        { return math.Round(x) }
func (h *MathHost) Sqrt(x float64) float64         { return math.Sqrt(x) }
func (h *MathHost) Pow(x, y float64) float64       { return math.Pow(x, y) }
func (h *MathHost) Min(x, y float64) float64       { return math.Min(x, y) }
func (h *MathHost) Max(x, y float64) float64       { return math.Max(x, y) }
func (h *MathHost) Sin(x float64) float64          { return math.Sin(x) }
func (h *MathHost) Cos(x float64) float64          { return math.Cos(x) }
func (h *MathHost) Tan(x float64) float64          { return math.Tan(x) }
func (h *MathHost) Exp(x float64) float64          { return math.Exp(x) }
func (h *MathHost) Log(x float64) float64          { return math.Log(x) }
func (h *MathHost) Log10(x float64) float64        { return math.Log10(x) }
func (h *MathHost) NaN() float64                   { return math.NaN() }
func (h *MathHost) IsNaN(f float64) bool           { return math.IsNaN(f) }
func (h *MathHost) Inf(sign int) float64           { return math.Inf(sign) }
func (h *MathHost) IsInf(f float64, sign int) bool { return math.IsInf(f, sign) }
