package engine_test

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

// TestRangeContinueWithNestedBlockScopes ensures that 'continue' inside
// a for-range loop does not corrupt the scope chain when the loop body
// contains nested block scopes (e.g. if-true blocks) situated after the
// 'continue' point.  The bug was that during UnwindContinue every
// OpScopeExit in the remaining body was re-executed, including those
// whose matching OpScopeEnter had been skipped.  This would pop scope
// levels that had never been pushed, causing outer-scope variables to
// become undefined.

func TestRangeContinueNestedBlockKeepsOuterVars(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s + "|"
}

func pair(v Int64) (Int64, Int64) {
	return v, 0
}

func main() {
	rowScan := 0
	nextPage := true

	for nextPage {
		mark("page-loop")

		for _, day := range []Int64{12, 12, 9} {
			rowScan++
			mark("row-" + string(rowScan))

			startData, err := pair(1)
			endData, err := pair(9)
			fabuDate, err := pair(day)
			if err != 0 {
				mark("err")
			}

			if endData < fabuDate {
				mark("continue-" + string(rowScan))
				continue
			}

			if fabuDate < startData {
				nextPage = false
				break
			}

			mark("keep-" + string(rowScan))

			if true {
				mark("inside-if")
			}

			nextPage = false
		}
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := executeAndSnapshot(t, prog)
	expected := "page-loop|row-1|continue-1|row-2|continue-2|row-3|keep-3|inside-if|\n"
	trace, ok := snapshot.LoadGlobal("trace")
	if !ok || trace == nil || trace.Str+"\n" != expected {
		t.Errorf("unexpected trace:\n  got: %#v\n  want: %q", trace, expected)
	}
}

// Same scenario but without the outer for-loop – purely range + continue +
// nested if-true block.
func TestRangeContinueNestedBlockKeepsOuterVarsNoOuterFor(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s + "|"
}

func pair(v Int64) (Int64, Int64) {
	return v, 0
}

func main() {
	rowScan := 0

	for _, day := range []Int64{12, 12, 9} {
		rowScan++
		mark("row-" + string(rowScan))

		startData, err := pair(1)
		endData, err := pair(9)
		fabuDate, err := pair(day)
		if err != 0 {
			mark("err")
		}

		if endData < fabuDate {
			mark("continue-" + string(rowScan))
			continue
		}

		if fabuDate < startData {
			break
		}

		mark("keep-" + string(rowScan))

		if true {
			mark("inside-if")
		}
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := executeAndSnapshot(t, prog)
	expected := "row-1|continue-1|row-2|continue-2|row-3|keep-3|inside-if|\n"
	trace, ok := snapshot.LoadGlobal("trace")
	if !ok || trace == nil || trace.Str+"\n" != expected {
		t.Errorf("unexpected trace:\n  got: %#v\n  want: %q", trace, expected)
	}
}
