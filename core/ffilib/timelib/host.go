package timelib

import (
	"time"
)

type TimeHost struct{}

func (h *TimeHost) Now() string {
	return time.Now().Format(time.RFC3339)
}

func (h *TimeHost) Unix() int64 {
	return time.Now().Unix()
}

func (h *TimeHost) UnixNano() int64 {
	return time.Now().UnixNano()
}

func (h *TimeHost) Sleep(ns int64) {
	time.Sleep(time.Duration(ns))
}

func (h *TimeHost) Since(ns int64) int64 {
	t := time.Unix(0, ns)
	return int64(time.Since(t))
}
