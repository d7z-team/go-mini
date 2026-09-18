package compiler

import (
	"fmt"
)

// OptimizationLevel selects the optional code-generation passes applied after
// typed HIR lowering. It does not affect parsing, semantic analysis, package
// reachability, or bytecode validation.
type OptimizationLevel uint8

const (
	// OptimizationNone preserves the control flow produced by lowering.
	OptimizationNone OptimizationLevel = iota
	// OptimizationDefault enables the standard deterministic optimization passes.
	OptimizationDefault
	// OptimizationFull enables additional proven function-local optimizations.
	OptimizationFull
)

func validateOptimizationLevel(level OptimizationLevel) error {
	if level > OptimizationFull {
		return fmt.Errorf("unsupported compiler optimization level %d", level)
	}
	return nil
}

// ParseOptimizationLevel validates a numeric optimization level supplied by a
// command or portable compiler client.
func ParseOptimizationLevel(level int) (OptimizationLevel, error) {
	if level < 0 || level > int(OptimizationFull) {
		return 0, fmt.Errorf("unsupported compiler optimization level %d", level)
	}
	return OptimizationLevel(level), nil
}
