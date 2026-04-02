//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg filepathlib -out filepath_ffigen.go interface.go
package filepathlib

// Filepath 接口定义了路径处理操作

// ffigen:module filepath
type Filepath interface {
	Base(path string) string
	Clean(path string) string
	Dir(path string) string
	Ext(path string) string
	IsAbs(path string) bool
	Join(elem ...string) string
	Match(pattern, name string) (bool, error)
	Rel(basepath, targpath string) (string, error)
	Split(path string) (string, string)
	ToSlash(path string) string
	FromSlash(path string) string
	VolumeName(path string) string
}
