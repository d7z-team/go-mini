//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg e2e -path gopkg.d7z.net/go-mini/core/e2e -out reverse_ffigen_test.go reverse_interface.go
package e2e

// ffigen:reverse
type ScriptCalculator interface {
	Add(a, b int64) int64
	Format(prefix string, val int64) string
	Divide(a, b int64) (int64, error)
}
