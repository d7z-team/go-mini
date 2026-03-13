package types

import (
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"gopkg.d7z.net/go-mini/core/ast"
)

// MiniFileInfo 包装 os.FileInfo
type MiniFileInfo struct {
	info os.FileInfo
}

func NewMiniFileInfo(info os.FileInfo) *MiniFileInfo {
	return &MiniFileInfo{info: info}
}

func (o *MiniFileInfo) OPSType() ast.Ident {
	return "FileInfo"
}

func (o *MiniFileInfo) Name() ast.MiniString {
	return ast.NewMiniString(o.info.Name())
}

func (o *MiniFileInfo) Size() ast.MiniInt64 {
	return ast.NewMiniInt64(o.info.Size())
}

func (o *MiniFileInfo) IsDir() ast.MiniBool {
	return ast.NewMiniBool(o.info.IsDir())
}

func (o *MiniFileInfo) ModTime() *MiniTime {
	return NewMiniTime(o.info.ModTime())
}

// MiniFsFile 包装 afero.File
type MiniFsFile struct {
	file afero.File
}

func NewMiniFsFile(file afero.File) *MiniFsFile {
	return &MiniFsFile{file: file}
}

func (o *MiniFsFile) OPSType() ast.Ident {
	return "FsFile"
}

func (o *MiniFsFile) Read(p []byte) (n int, err error) {
	return o.file.Read(p)
}

func (o *MiniFsFile) Write(p []byte) (n int, err error) {
	return o.file.Write(p)
}

func (o *MiniFsFile) MiniRead(n *ast.MiniInt64) (MiniFile, error) {
	buf := make([]byte, n.GoValue().(int64))
	read, err := o.file.Read(buf)
	if err != nil && err != io.EOF {
		return MiniFile{}, err
	}
	return NewMiniFile(buf[:read]), nil
}

func (o *MiniFsFile) MiniWrite(data *MiniFile) (ast.MiniInt64, error) {
	n, err := o.file.Write(data.Bytes())
	return ast.NewMiniInt64(int64(n)), err
}

func (o *MiniFsFile) Close() error {
	return o.file.Close()
}

func (o *MiniFsFile) Stat() (*MiniFileInfo, error) {
	info, err := o.file.Stat()
	if err != nil {
		return nil, err
	}
	return NewMiniFileInfo(info), nil
}

// MiniFs 包装 afero.Fs
type MiniFs struct {
	fs afero.Fs
}

func NewMiniFs(fs afero.Fs) *MiniFs {
	return &MiniFs{fs: fs}
}

func (o *MiniFs) OPSType() ast.Ident {
	return "Fs"
}

func (o *MiniFs) Mkdir(path *ast.MiniString, perm *ast.MiniInt64) error {
	return o.fs.Mkdir(path.GoString(), osFileMode(perm))
}

func (o *MiniFs) MkdirAll(path *ast.MiniString) error {
	return o.fs.MkdirAll(path.GoString(), 0o755)
}

func (o *MiniFs) Remove(path *ast.MiniString) error {
	return o.fs.RemoveAll(path.GoString())
}

func (o *MiniFs) RemoveAll(path *ast.MiniString) error {
	return o.fs.RemoveAll(path.GoString())
}

func (o *MiniFs) Exists(path *ast.MiniString) (ast.MiniBool, error) {
	exists, err := afero.Exists(o.fs, path.GoString())
	return ast.NewMiniBool(exists), err
}

func (o *MiniFs) IsDir(path *ast.MiniString) (ast.MiniBool, error) {
	isDir, err := afero.IsDir(o.fs, path.GoString())
	return ast.NewMiniBool(isDir), err
}

func (o *MiniFs) ReadFile(path *ast.MiniString) (MiniFile, error) {
	data, err := afero.ReadFile(o.fs, path.GoString())
	if err != nil {
		return MiniFile{}, err
	}
	return NewMiniFile(data), nil
}

func (o *MiniFs) WriteFile(path *ast.MiniString, file *MiniFile) error {
	return afero.WriteFile(o.fs, path.GoString(), file.Bytes(), 0o644)
}

func (o *MiniFs) ReadString(path *ast.MiniString) (ast.MiniString, error) {
	data, err := afero.ReadFile(o.fs, path.GoString())
	if err != nil {
		return ast.MiniString{}, err
	}
	return ast.NewMiniString(string(data)), nil
}

func (o *MiniFs) WriteString(path, content *ast.MiniString) error {
	return afero.WriteFile(o.fs, path.GoString(), []byte(content.GoString()), 0o644)
}

func (o *MiniFs) ReadDir(path *ast.MiniString) ([]*MiniFileInfo, error) {
	files, err := afero.ReadDir(o.fs, path.GoString())
	if err != nil {
		return nil, err
	}
	var result []*MiniFileInfo
	for _, f := range files {
		result = append(result, NewMiniFileInfo(f))
	}
	return result, nil
}

func (o *MiniFs) Copy(src, dst *ast.MiniString) error {
	source, err := o.fs.Open(src.GoString())
	if err != nil {
		return err
	}
	defer source.Close()

	err = o.fs.MkdirAll(filepath.Dir(dst.GoString()), 0o755)
	if err != nil {
		return err
	}

	destination, err := o.fs.Create(dst.GoString())
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

func (o *MiniFs) Move(src, dst *ast.MiniString) error {
	err := o.fs.Rename(src.GoString(), dst.GoString())
	if err == nil {
		return nil
	}
	err = o.Copy(src, dst)
	if err != nil {
		return err
	}
	return o.fs.Remove(src.GoString())
}

func (o *MiniFs) Rename(oldpath, newpath *ast.MiniString) error {
	return o.fs.Rename(oldpath.GoString(), newpath.GoString())
}

func (o *MiniFs) Stat(path *ast.MiniString) (*MiniFileInfo, error) {
	info, err := o.fs.Stat(path.GoString())
	if err != nil {
		return nil, err
	}
	return NewMiniFileInfo(info), nil
}

func (o *MiniFs) Chmod(path *ast.MiniString, mode *ast.MiniInt64) error {
	return o.fs.Chmod(path.GoString(), osFileMode(mode))
}

func (o *MiniFs) Chtimes(path *ast.MiniString, atime, mtime *MiniTime) error {
	return o.fs.Chtimes(path.GoString(), atime.t, mtime.t)
}

func (o *MiniFs) Create(path *ast.MiniString) (*MiniFsFile, error) {
	f, err := o.fs.Create(path.GoString())
	if err != nil {
		return nil, err
	}
	return NewMiniFsFile(f), nil
}

func (o *MiniFs) Open(path *ast.MiniString) (*MiniFsFile, error) {
	f, err := o.fs.Open(path.GoString())
	if err != nil {
		return nil, err
	}
	return NewMiniFsFile(f), nil
}

func osFileMode(n *ast.MiniInt64) os.FileMode {
	if n == nil {
		return 0o644
	}
	return os.FileMode(n.GoValue().(int64))
}
