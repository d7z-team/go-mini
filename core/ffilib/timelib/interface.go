//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg timelib -path gopkg.d7z.net/go-mini/core/ffilib/timelib -out time_ffigen.go interface.go
package timelib

// Time 接口定义了时间操作

// ffigen:module time
type Time interface {
	Now() string
	Unix() int64
	UnixMilli() int64
	UnixMicro() int64
	UnixNano() int64
	Sleep(ns int64)
	Since(ns int64) int64
	Format(ns int64, layout string) string
	Parse(layout, value string) (int64, error)
	ParseDuration(s string) (int64, error)
	Add(ns, duration int64) int64
	Sub(ns1, ns2 int64) int64
}
