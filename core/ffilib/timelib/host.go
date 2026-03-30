package timelib

import (
	"time"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// TimeHost 实现 Module 接口
type TimeHost struct{}

func (h *TimeHost) Now() *Time {
	return &Time{T: time.Now()}
}

func (h *TimeHost) Unix(sec, nsec int64) *Time {
	return &Time{T: time.Unix(sec, nsec)}
}

func (h *TimeHost) Sleep(ns int64) {
	time.Sleep(time.Duration(ns))
}

func (h *TimeHost) Since(t *Time) int64 {
	if t == nil {
		return 0
	}
	return int64(time.Since(t.T))
}

func (h *TimeHost) Until(t *Time) int64 {
	if t == nil {
		return 0
	}
	return int64(time.Until(t.T))
}

func (h *TimeHost) Parse(layout, value string) (*Time, error) {
	t, err := time.Parse(layout, value)
	if err != nil {
		return nil, err
	}
	return &Time{T: t}, nil
}

func (h *TimeHost) ParseDuration(s string) (int64, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return int64(d), nil
}

// Time 是时间句柄
// ffigen:methods
type Time struct {
	T time.Time
}

func (t *Time) Year() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Year())
}

func (t *Time) Month() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Month())
}

func (t *Time) Day() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Day())
}

func (t *Time) Hour() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Hour())
}

func (t *Time) Minute() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Minute())
}

func (t *Time) Second() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Second())
}

func (t *Time) Nanosecond() int64 {
	if t == nil {
		return 0
	}
	return int64(t.T.Nanosecond())
}

func (t *Time) Unix() int64 {
	if t == nil {
		return 0
	}
	return t.T.Unix()
}

func (t *Time) UnixMilli() int64 {
	if t == nil {
		return 0
	}
	return t.T.UnixMilli()
}

func (t *Time) UnixMicro() int64 {
	if t == nil {
		return 0
	}
	return t.T.UnixMicro()
}

func (t *Time) UnixNano() int64 {
	if t == nil {
		return 0
	}
	return t.T.UnixNano()
}

func (t *Time) Format(layout string) string {
	if t == nil {
		return ""
	}
	return t.T.Format(layout)
}

func (t *Time) Add(d int64) *Time {
	if t == nil {
		return nil
	}
	return &Time{T: t.T.Add(time.Duration(d))}
}

func (t *Time) Sub(other *Time) int64 {
	if t == nil || other == nil {
		return 0
	}
	return int64(t.T.Sub(other.T))
}

func (t *Time) IsZero() bool {
	if t == nil {
		return true
	}
	return t.T.IsZero()
}

func (t *Time) Before(other *Time) bool {
	if t == nil || other == nil {
		return false
	}
	return t.T.Before(other.T)
}

func (t *Time) After(other *Time) bool {
	if t == nil || other == nil {
		return false
	}
	return t.T.After(other.T)
}

func (t *Time) Equal(other *Time) bool {
	if t == nil || other == nil {
		return false
	}
	return t.T.Equal(other.T)
}

func (t *Time) String() string {
	if t == nil {
		return ""
	}
	return t.T.String()
}

// RegisterTimeAll 注册所有时间相关的 FFI
func RegisterTimeAll(executor interface {
	RegisterFFI(string, ffigo.FFIBridge, uint32, ast.GoMiniType, string)
	RegisterStructSpec(string, ast.GoMiniType)
	RegisterConstant(string, string)
}, impl Module, registry *ffigo.HandleRegistry,
) {
	RegisterModule(executor, impl, registry)
	RegisterTime(executor, registry)
}
