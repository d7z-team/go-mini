//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg synclib -out sync_ffigen.go interface.go host.go
package synclib

// ffigen:module sync
type Module interface {
	NewWaitGroup() *WaitGroup
}
