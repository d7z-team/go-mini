//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg iolib -out io_ffigen.go interface.go
package iolib

type IO interface {
	ReadAll(r any) ([]byte, error)
}
