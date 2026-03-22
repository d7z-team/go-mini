package storagelib

//go:generate go run ../../../cmd/ffigen/main.go -pkg storagelib -out storage_ffigen.go interface.go

// ffigen:module storage
type StorageAPI interface {
	SetCapacity(cap uint32)
	GetStatus() int16
}
