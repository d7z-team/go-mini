//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg mathlib -path gopkg.d7z.net/go-mini/core/ffilib/mathlib -out math_ffigen.go interface.go
package mathlib

// Math 接口定义了数学计算操作

// ffigen:module math
type Math interface {
	Abs(x float64) float64
	Ceil(x float64) float64
	Floor(x float64) float64
	Round(x float64) float64
	Sqrt(x float64) float64
	Pow(x, y float64) float64
	Min(x, y float64) float64
	Max(x, y float64) float64
	Sin(x float64) float64
	Cos(x float64) float64
	Tan(x float64) float64
	Exp(x float64) float64
	Log(x float64) float64
	Log10(x float64) float64
	NaN() float64
	IsNaN(f float64) bool
	Inf(sign int) float64
	IsInf(f float64, sign int) bool
	Pi() float64
	E() float64
}
