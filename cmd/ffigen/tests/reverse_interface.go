//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg tests -out reverse_ffigen_test.go reverse_interface.go
package tests

// ffigen:reverse
type ScriptCalculator interface {
	Add(a, b int64) int64
	Format(prefix string, val int64) string
	Divide(a, b int64) (int64, error)
}
