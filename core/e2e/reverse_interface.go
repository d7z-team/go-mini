package e2e

// ffigen:reverse
type ScriptCalculator interface {
	Add(a, b int) int
	Format(prefix string, val int) string
	Divide(a, b int) (int, error)
}
