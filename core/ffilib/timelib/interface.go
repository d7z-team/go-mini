//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg timelib -path gopkg.d7z.net/go-mini/core/ffilib/timelib -out time_ffigen.go interface.go host.go
package timelib

import (
	"time"
)

// Module 接口定义了时间模块的操作
// ffigen:module time
const (
	Nanosecond  = time.Nanosecond
	Microsecond = time.Microsecond
	Millisecond = time.Millisecond
	Second      = time.Second
	Minute      = time.Minute
	Hour        = time.Hour

	Layout      = time.Layout
	ANSIC       = time.ANSIC
	UnixDate    = time.UnixDate
	RubyDate    = time.RubyDate
	RFC822      = time.RFC822
	RFC822Z     = time.RFC822Z
	RFC850      = time.RFC850
	RFC1123     = time.RFC1123
	RFC1123Z    = time.RFC1123Z
	RFC3339     = time.RFC3339
	RFC3339Nano = time.RFC3339Nano
	Kitchen     = time.Kitchen
)

// ffigen:module time
type Module interface {
	Now() *Time
	Unix(sec, nsec int64) *Time
	Sleep(ns int64)
	Since(t *Time) int64
	Until(t *Time) int64
	Parse(layout, value string) (*Time, error)
	ParseDuration(s string) (int64, error)
}
