package timelib

import (
	"time"
)

type TimeHost struct{}

func (h *TimeHost) Now() string {
	return time.Now().Format(time.RFC3339)
}

func (h *TimeHost) Sleep(ns int64) {
	// 注意：在隔离架构下，FFI 调用应当感知 Context
	// 暂时简单实现，后续可优化为传递 ctx
	time.Sleep(time.Duration(ns))
}

func (h *TimeHost) Since(startRFC3339 string) int64 {
	t, _ := time.Parse(time.RFC3339, startRFC3339)
	return int64(time.Since(t))
}
