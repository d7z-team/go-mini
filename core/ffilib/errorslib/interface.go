//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg errorslib -out errors_ffigen.go interface.go
package errorslib

// Errors 接口定义了错误创建操作

// ffigen:module errors
type Errors interface {
	New(text string) error
}
