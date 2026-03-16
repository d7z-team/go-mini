package oslib

type File struct{}

type OS interface {
	Open(name string) (*File, error)
	Create(name string) (*File, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte) error
	Remove(name string) error
	Close(f *File) error
}
