package engine_test

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

// 多个嵌套 if 块都在 continue 之后
func TestRangeContinueMultipleNestedBlocks(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main
import "fmt"

var trace = ""

func mark(s string) {
	trace = trace + s + "|"
}

func main() {
	rowScan := 0

	for _, day := range []Int64{12, 12, 9} {
		rowScan++
		mark("row-" + string(rowScan))

		startData, endData := 1, 9
		fabuDate := int(day)
		_ = fabuDate
		_ = startData
		_ = endData

		if endData < fabuDate {
			mark("continue-" + string(rowScan))
			continue
		}

		if fabuDate < startData {
			break
		}

		mark("keep-" + string(rowScan))

		if true {
			mark("block1-" + string(rowScan))
			if true {
				mark("block2-" + string(rowScan))
			}
		}
	}

	fmt.Println(trace)
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	output := executeWithCapturedOutput(t, prog)
	expected := "row-1|continue-1|row-2|continue-2|row-3|keep-3|block1-3|block2-3|\n"
	if output != expected {
		t.Errorf("unexpected output:\n  got: %q\n  want: %q", output, expected)
	}
}

// 传统 for 循环中的 continue + 嵌套块
func TestForContinueWithNestedBlocks(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main
import "fmt"

var trace = ""

func mark(s string) {
	trace = trace + s + "|"
}

func main() {
	outer := 0

	for i := 0; i < 6; i++ {
		outer++
		if i < 2 {
			mark("skip-" + string(outer))
			continue
		}

		if i > 4 {
			break
		}

		mark("keep-" + string(outer))

		if true {
			mark("block-" + string(outer))
		}
	}

	fmt.Println(trace)
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	output := executeWithCapturedOutput(t, prog)
	expected := "skip-1|skip-2|keep-3|block-3|keep-4|block-4|keep-5|block-5|\n"
	if output != expected {
		t.Errorf("unexpected output:\n  got: %q\n  want: %q", output, expected)
	}
}

// break 在内层 range 中，外层变量应仍然可访问
func TestRangeBreakWithNestedBlocks(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main
import "fmt"

var trace = ""

func mark(s string) {
	trace = trace + s + "|"
}

func main() {
	rowScan := 0
	nextPage := true

	for nextPage {
		mark("page-loop")

		for _, day := range []Int64{12, 9, 5, 1} {
			rowScan++
			mark("row-" + string(rowScan))

			if day < 10 {
				mark("break-" + string(rowScan))
				nextPage = false
				break
			}

			if true {
				mark("block-" + string(rowScan))
			}
		}
	}

	fmt.Println("rowScan=", rowScan)
	fmt.Println(trace)
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	output := executeWithCapturedOutput(t, prog)
	expected := "rowScan= 2\npage-loop|row-1|block-1|row-2|break-2|\n"
	if output != expected {
		t.Errorf("unexpected output:\n  got: %q\n  want: %q", output, expected)
	}
}

// for 循环中的 break + 嵌套块
func TestForBreakWithNestedBlocks(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main
import "fmt"

var trace = ""

func mark(s string) {
	trace = trace + s + "|"
}

func main() {
	outer := 0

	for i := 0; i < 10; i++ {
		outer++
		if i > 2 {
			mark("break-" + string(outer))
			break
		}

		mark("keep-" + string(outer))

		if true {
			mark("block-" + string(outer))
		}
	}

	fmt.Println("outer=", outer)
	fmt.Println(trace)
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	output := executeWithCapturedOutput(t, prog)
	expected := "outer= 4\nkeep-1|block-1|keep-2|block-2|keep-3|block-3|break-4|\n"
	if output != expected {
		t.Errorf("unexpected output:\n  got: %q\n  want: %q", output, expected)
	}
}
