package e2e

type File struct {
	Name string
}

type FileInfo struct {
	Size uint32
	Name string
}

type Nested struct {
	Info  FileInfo
	Level int
}

type MockOS interface {
	Open(name string) (*File, error)
	Name(f *File) string
	Stat(f *File) (FileInfo, error)
	Read(f *File, b []byte) (int, error)
	Write(f *File, b []byte) (int, error)
	Close(f *File) error
	Deep(n Nested) Nested
}
