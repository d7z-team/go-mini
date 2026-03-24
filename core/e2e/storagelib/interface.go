package storagelib

//go:generate go run ../../../cmd/ffigen/main.go -pkg storagelib -path gopkg.d7z.net/go-mini/core/e2e/storagelib -out storage_ffigen.go interface.go

// ffigen:module storage
type StorageAPI interface {
	SetCapacity(capacity uint32)
	GetStatus() int16
}
