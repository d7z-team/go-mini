package workspace

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/parser"
	"github.com/d7z-team/mini-go/compiler/scanner"
)

const (
	DefaultMaxPackages         = 4096
	DefaultMaxFiles            = 32768
	DefaultMaxTotalSourceBytes = 256 << 20
	DefaultMaxDiagnostics      = 100
)

// Limits bounds deterministic work performed while loading a source graph.
// Zero values select defaults; callers may supply lower values.
type Limits struct {
	MaxPackages         int
	MaxFiles            int
	MaxTotalSourceBytes int
	MaxSourceBytes      int
	MaxTokens           int
	MaxSyntaxDepth      int
	MaxASTNodes         int
	MaxDiagnostics      int
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxPackages <= 0 || limits.MaxPackages > DefaultMaxPackages {
		limits.MaxPackages = DefaultMaxPackages
	}
	if limits.MaxFiles <= 0 || limits.MaxFiles > DefaultMaxFiles {
		limits.MaxFiles = DefaultMaxFiles
	}
	if limits.MaxTotalSourceBytes <= 0 || limits.MaxTotalSourceBytes > DefaultMaxTotalSourceBytes {
		limits.MaxTotalSourceBytes = DefaultMaxTotalSourceBytes
	}
	if limits.MaxSourceBytes <= 0 || limits.MaxSourceBytes > scanner.DefaultMaxSourceBytes {
		limits.MaxSourceBytes = scanner.DefaultMaxSourceBytes
	}
	if limits.MaxTokens <= 0 || limits.MaxTokens > scanner.DefaultMaxTokens {
		limits.MaxTokens = scanner.DefaultMaxTokens
	}
	if limits.MaxSyntaxDepth <= 0 || limits.MaxSyntaxDepth > parser.DefaultMaxNesting {
		limits.MaxSyntaxDepth = parser.DefaultMaxNesting
	}
	if limits.MaxASTNodes <= 0 || limits.MaxASTNodes > ast.DefaultMaxNodes {
		limits.MaxASTNodes = ast.DefaultMaxNodes
	}
	if limits.MaxDiagnostics <= 0 || limits.MaxDiagnostics > DefaultMaxDiagnostics {
		limits.MaxDiagnostics = DefaultMaxDiagnostics
	}
	return limits
}

func (input Limits) parserLimits() parser.Limits {
	limits := normalizeLimits(input)
	return parser.Limits{
		Scanner: scanner.Limits{
			MaxSourceBytes: limits.MaxSourceBytes,
			MaxTokens:      limits.MaxTokens,
			MaxDiagnostics: limits.MaxDiagnostics,
		},
		MaxNesting: limits.MaxSyntaxDepth, MaxASTNodes: limits.MaxASTNodes, MaxDiagnostics: limits.MaxDiagnostics,
	}
}
