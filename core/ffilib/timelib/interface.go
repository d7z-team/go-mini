//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg timelib -out time_ffigen.go interface.go
package timelib

// Time 接口定义了时间操作
//
// ffigen:module time
type Time interface {
	Now() string
	Sleep(ns int64)
	Since(startRFC3339 string) int64
}
