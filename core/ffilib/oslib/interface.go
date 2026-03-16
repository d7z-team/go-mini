package oslib

type File struct{}

type OS interface {
	Open(name string) (*File, error)
	Create(name string) (*File, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte) error
	Remove(name string) error
	Read(f *File, b []byte) (int, error)
	Write(f *File, b []byte) (int, error)
	Close(f *File) error
}
