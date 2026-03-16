package timelib

type Time interface {
	Now() string
	Sleep(ns int64)
	Since(startRFC3339 string) int64
}
