//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg timelib -out time_ffigen.go interface.go
package timelib

type Time interface {
	Now() string
	Sleep(ns int64)
	Since(startRFC3339 string) int64
}
