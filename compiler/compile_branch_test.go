package compiler

import "testing"

func TestCompileSourceValidatesGotoTargetsAndScopes(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "undefined label",
			source: `package main
func Main() int64 {
	goto missing
	return 0
}`,
			code: "ast.branch.goto.label.unknown",
		},
		{
			name: "duplicate label",
			source: `package main
func Main() int64 {
	done:
	return 1
	done:
	return 2
}`,
			code: "ast.stmt.label.duplicate",
		},
		{
			name: "jump into block",
			source: `package main
func Main() int64 {
	goto inside
	if true {
	inside:
		return 1
	}
	return 0
}`,
			code: "ast.branch.goto.scope",
		},
		{
			name: "jump over declaration",
			source: `package main
func Main() int64 {
	goto done
	value := int64(1)
	done:
	return 42
}`,
			code: "ast.branch.goto.decl",
		},
		{
			name: "unused label",
			source: `package main
func Main() int64 {
unused:
	return 42
}`,
			code: "ast.stmt.label.unused",
		},
		{
			name: "goto cannot target nested function label",
			source: `package main
func Main() int64 {
	_ = func() int64 {
	inner:
		return 1
	}
	goto inner
	return 0
}`,
			code: "ast.branch.goto.label.unknown",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/goto", "main.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			if result.OK() {
				t.Fatalf("expected diagnostic %s", test.code)
			}
			for _, diagnostic := range result.Diagnostics {
				if string(diagnostic.Code) == test.code {
					return
				}
			}
			t.Fatalf("expected diagnostic %s, got %#v", test.code, result.Diagnostics)
		})
	}
}

func TestCompileSourceValidatesMissingReturnPaths(t *testing.T) {
	tests := []struct {
		name   string
		source string
		valid  bool
	}{
		{
			name: "if without else can fall through",
			source: `package main
func Main() int64 {
		if true { return 1 }
}`,
		},
		{
			name: "finite loop can fall through",
			source: `package main
func Main() int64 {
		for i := 0; i < 1; i++ { return 1 }
}`,
		},
		{
			name: "labeled break exits infinite loop",
			source: `package main
func Main() int64 {
outer:
		for { break outer }
}`,
		},
		{
			name: "named result assignment is not an implicit return",
			source: `package main
func Main() (value int64) {
		value = 1
}`,
		},
		{
			name: "if else terminates",
			source: `package main
func Main() int64 {
		if true { return 1 } else { panic("unreachable") }
}`,
			valid: true,
		},
		{
			name: "switch default terminates",
			source: `package main
func Main() int64 {
		switch int64(1) { case 1: return 1; default: return 2 }
}`,
			valid: true,
		},
		{
			name: "switch fallthrough terminates",
			source: `package main
func Main() int64 {
		switch int64(1) { case 1: fallthrough; default: return 2 }
}`,
			valid: true,
		},
		{
			name: "empty select terminates",
			source: `package main
func Main() int64 {
		select {}
}`,
			valid: true,
		},
		{
			name: "infinite loop terminates",
			source: `package main
func Main() int64 {
		for {}
}`,
			valid: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/returns", "main.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			missing := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "ast.func.return.missing" {
					missing = true
					break
				}
			}
			if test.valid && missing {
				t.Fatalf("unexpected missing-return diagnostic: %#v", result.Diagnostics)
			}
			if !test.valid && !missing {
				t.Fatalf("expected ast.func.return.missing, got %#v", result.Diagnostics)
			}
		})
	}
}

func TestCompileSourceValidatesBranchTargetsAndFallthrough(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "break outside control flow",
			source: `package main
func Main() int64 {
		break
		return 0
}`,
			code: "ast.branch.break.outside",
		},
		{
			name: "continue outside loop",
			source: `package main
func Main() int64 {
		continue
		return 0
}`,
			code: "ast.branch.continue.outside",
		},
		{
			name: "break label target is not breakable",
			source: `package main
func Main() int64 {
target:
		{
			break target
		}
		return 0
}`,
			code: "ast.branch.break.label.target",
		},
		{
			name: "continue label target is not a loop",
			source: `package main
func Main() int64 {
target:
		switch int64(1) { default: continue target }
	return 0
}`,
			code: "ast.branch.continue.label.target",
		},
		{
			name: "break label is not enclosing",
			source: `package main
func Main() int64 {
target:
	for { break }
	for { break target }
	return 0
}`,
			code: "ast.branch.break.label.scope",
		},
		{
			name: "fallthrough outside switch",
			source: `package main
func Main() int64 {
		fallthrough
		return 0
}`,
			code: "ast.branch.fallthrough.context",
		},
		{
			name: "fallthrough final case",
			source: `package main
func Main() int64 {
		switch int64(1) { default: fallthrough }
		return 0
}`,
			code: "ast.branch.fallthrough.final",
		},
		{
			name: "fallthrough must be last",
			source: `package main
func Main() int64 {
		switch int64(1) { case 1: fallthrough; return 1; default: return 2 }
}`,
			code: "ast.branch.fallthrough.position",
		},
		{
			name: "type switch forbids fallthrough",
			source: `package main
func Main(value any) int64 {
		switch value.(type) { case int64: fallthrough; default: return 1 }
}`,
			code: "ast.branch.fallthrough.typeswitch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/branches", "main.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			for _, diagnostic := range result.Diagnostics {
				if string(diagnostic.Code) == test.code {
					return
				}
			}
			t.Fatalf("expected diagnostic %s, got %#v", test.code, result.Diagnostics)
		})
	}
}

func TestCompileSourceKeepsNestedFunctionLabelScopesSeparate(t *testing.T) {
	result, err := compileTestSource("example/labels", "main.mgo", `package main
func Main() int64 {
	goto done
done:
	f := func() int64 {
		goto done
	done:
		return 41
	}
	return f() + 1
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}
