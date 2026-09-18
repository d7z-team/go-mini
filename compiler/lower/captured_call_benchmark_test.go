package lower

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/parser"
)

func BenchmarkAnalyzeAndLowerCapturedCalls(b *testing.B) {
	const source = `package example
type Counter struct { N int }
func (c Counter) Send(out chan int, values ...int) {
 for _, value := range values { c.N += value }
 out <- c.N
}
type Sender interface { Send(chan int, ...int) }
func Launch(c Counter, s Sender, out chan int, values []int) {
 go c.Send(out, 1, 2)
 go s.Send(out, values...)
 defer c.Send(out, 3, 4)
 defer s.Send(out, values...)
}
`

	parsed := parser.ParseSource("example", "calls.mgo", source)
	if len(parsed.Diagnostics) != 0 {
		b.Fatal(parsed.Diagnostics)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, diagnostics := lowerTestProgram(parsed.Program); len(diagnostics) != 0 {
			b.Fatal(diagnostics)
		}
	}
}
