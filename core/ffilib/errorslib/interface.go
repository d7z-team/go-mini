//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg errorslib -out errors_ffigen.go interface.go
package errorslib

type Errors interface {
	New(text string) error
}
