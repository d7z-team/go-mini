package rpc

import (
	"strconv"
	"testing"
	"time"
)

func BenchmarkEndpointLeaseBatch(b *testing.B) {
	for _, count := range []int{32, 1024, 4096} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			now := time.Now()
			bindings, operations := make(map[uint64]time.Time), make(map[uint64]time.Time)
			for id := 1; id <= count; id++ {
				bindings[uint64(id)] = now.Add(time.Hour)
				operations[uint64(id)] = now.Add(time.Hour)
			}
			offset := 0
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				all := collectLeaseTargets(bindings, operations, now)
				payload := encodeLeaseTargetsBatch(all, 128, &offset)
				if len(all) != count*2 || len(payload) == 0 {
					b.Fatal("invalid renewal batch")
				}
			}
		})
	}
}
