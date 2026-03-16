package e2e

type File struct {
	Name string
}

type FileInfo struct {
	Size uint32
	Name string
}

type OS interface {
	Open(name string) (*File, error)
	Name(f *File) string
	Stat(f *File) (FileInfo, error)
	Close(f *File) error
}
