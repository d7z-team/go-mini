package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestSwitchContinuePropagatesToOuterLoop(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s
}

func main() {
	sum := 0
	for i := 0; i < 4; i++ {
		switch i {
		case 1:
			mark("c")
			continue
		case 3:
			mark("d")
		}
		sum = sum + i
		mark("x")
	}
	if sum != 5 {
		panic("sum mismatch")
	}
	if trace != "xcxdx" {
		panic("trace mismatch: " + trace)
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFunctionDeferSurvivesLoopControlFlow(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s
}

func demo() {
	for i := 0; i < 4; i++ {
		if i == 0 {
			defer mark("0")
		} else if i == 1 {
			defer mark("1")
		} else if i == 2 {
			defer mark("2")
		} else {
			defer mark("3")
		}
		if i == 1 {
			continue
		}
		if i == 3 {
			break
		}
		mark("x")
	}
}

func main() {
	demo()
	if trace != "xx3210" {
		panic("trace mismatch: " + trace)
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSwitchBreakStaysInsideInnerLoop(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s
}

func main() {
	sum := 0
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			switch j {
			case 1:
				mark("b")
				break
			}
			sum = sum + 1
			mark("x")
		}
		mark("o")
	}
	if sum != 6 {
		panic("sum mismatch")
	}
	if trace != "xbxxoxbxxo" {
		panic("trace mismatch: " + trace)
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSwitchContinueTargetsNearestInnerLoop(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s
}

func main() {
	sum := 0
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			switch j {
			case 1:
				mark("c")
				continue
			}
			sum = sum + 1
			mark("x")
		}
		mark("o")
	}
	if sum != 4 {
		panic("sum mismatch")
	}
	if trace != "xcxoxcxo" {
		panic("trace mismatch: " + trace)
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInnerRangeBreakDoesNotEscapeOuterLoops(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s
}

func main() {
	sum := 0
	for i := 0; i < 2; i++ {
		for _, v := range []Int64{0, 1, 2} {
			if v == 1 {
				mark("b")
				break
			}
			sum = sum + 1
			mark("x")
		}
		mark("o")
	}
	if sum != 2 {
		panic("sum mismatch")
	}
	if trace != "xboxbo" {
		panic("trace mismatch: " + trace)
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInnerRangeContinueDoesNotSkipOuterLoop(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s
}

func main() {
	sum := 0
	for i := 0; i < 2; i++ {
		for _, v := range []Int64{0, 1, 2} {
			if v == 1 {
				mark("c")
				continue
			}
			sum = sum + 1
			mark("x")
		}
		mark("o")
	}
	if sum != 4 {
		panic("sum mismatch")
	}
	if trace != "xcxoxcxo" {
		panic("trace mismatch: " + trace)
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRangeContinueSkipsTailAfterShortDeclsAndNestedBlock(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s + "|"
}

func pair(v int) (int, int) {
	return v, 0
}

func main() {
	for _, item := range []int{1} {
		startData, err := pair(10)
		endData, err := pair(20)
		fabuDate, err := pair(30)
		if err != 0 {
			mark("err")
		}
		mark("parsed")
		if endData < fabuDate {
			mark("continue")
			continue
		}
		if fabuDate < startData {
			break
		}
		name := "name"
		mark(name)
		if true {
			mark("click-title-before")
			mark("click-title-after")
			for true {
				mark("wait")
				break
			}
			mark("expect-download-before")
			mark("expect-download-after")
		}
		mark("table-setrow")
	}
	if trace != "parsed|continue|" {
		panic("trace mismatch: " + trace)
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}
