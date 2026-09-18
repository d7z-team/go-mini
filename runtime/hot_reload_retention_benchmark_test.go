package runtime

import (
	"fmt"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func BenchmarkRevisionRetentionSweep(b *testing.B) {
	for _, size := range []int{0, 1024, 16384} {
		b.Run(fmt.Sprintf("elements=%d", size), func(b *testing.B) {
			artifact := ir.NewArtifact("benchmark/retention", "main")
			artifact.Globals = []ir.Global{{ID: "global.values", Type: testType("Slice<Any>")}}
			machine, err := loadTestEngine(artifact)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(machine.closeRevisions)
			values := make([]vmValue, size)
			for i := range values {
				values[i] = newVMValue("Int", int64(i))
			}
			cell := machine.rootModule().state.globals["global.values"]
			cell.value, cell.initialized = newSliceValue("Slice<Any>", values), true
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				machine.sweepRetiredRevisions()
			}
		})
	}
}
