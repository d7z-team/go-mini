//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg timelib -out time_ffigen.go interface.go
package timelib

// Time 接口定义了时间操作
//
// ffigen:module time
type Time interface {
	Now() string
	Unix() int64
	UnixNano() int64
	Sleep(ns int64)
	Since(ns int64) int64
}
