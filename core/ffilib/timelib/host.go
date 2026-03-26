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

func (h *TimeHost) UnixMilli() int64 {
	return time.Now().UnixMilli()
}

func (h *TimeHost) UnixMicro() int64 {
	return time.Now().UnixMicro()
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

func (h *TimeHost) Format(ns int64, layout string) string {
	t := time.Unix(0, ns)
	return t.Format(layout)
}

func (h *TimeHost) Parse(layout, value string) (int64, error) {
	t, err := time.Parse(layout, value)
	if err != nil {
		return 0, err
	}
	return t.UnixNano(), nil
}

func (h *TimeHost) ParseDuration(s string) (int64, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return int64(d), nil
}

func (h *TimeHost) Add(ns, duration int64) int64 {
	t := time.Unix(0, ns)
	return t.Add(time.Duration(duration)).UnixNano()
}

func (h *TimeHost) Sub(ns1, ns2 int64) int64 {
	t1 := time.Unix(0, ns1)
	t2 := time.Unix(0, ns2)
	return int64(t1.Sub(t2))
}
