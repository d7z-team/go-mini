package storagelib

//go:generate go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg storagelib -out storage_ffigen.go interface.go

// ffigen:module storage
type StorageAPI interface {
	SetCapacity(capacity uint32)
	GetStatus() int16
}
