package runtimes

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

// --- File Types ---

type MiniFile struct {
	data []byte
}

func NewMiniFile(data []byte) MiniFile {
	return MiniFile{data: data}
}

func (o *MiniFile) GoMiniType() ast.Ident      { return "io.File" }
func (o *MiniFile) Size() ast.MiniInt64        { return ast.NewMiniInt64(int64(len(o.data))) }
func (o *MiniFile) Bytes() []byte              { return o.data }
func (o *MiniFile) ReadString() ast.MiniString { return ast.NewMiniString(string(o.data)) }
func (o *MiniFile) GoString() string           { return string(o.data) }
func (o *MiniFile) Clone() ast.MiniObj {
	newData := make([]byte, len(o.data))
	copy(newData, o.data)
	return &MiniFile{data: newData}
}

func (o *MiniFile) Named(name *ast.MiniString) MiniNamedFile {
	return NewMiniNamedFile(name.GoString(), o.data)
}

func (o *MiniFile) New(static string) (ast.MiniObj, error) {
	return &MiniFile{data: []byte(static)}, nil
}

type MiniNamedFile struct {
	name string
	file MiniFile
}

func NewMiniNamedFile(name string, data []byte) MiniNamedFile {
	return MiniNamedFile{
		name: filepath.Base(name),
		file: NewMiniFile(data),
	}
}

func (o *MiniNamedFile) Name() ast.MiniString { return ast.NewMiniString(o.name) }
func (o *MiniNamedFile) GOFileName() string   { return o.name }
func (o *MiniNamedFile) String() ast.MiniString {
	return ast.NewMiniString(fmt.Sprintf("File(name=%s, size=%d)", o.name, len(o.file.data)))
}
func (o *MiniNamedFile) GoMiniType() ast.Ident { return "io.NamedFile" }
func (o *MiniNamedFile) Bytes() []byte         { return o.file.data }

// --- FS Types ---

type MiniFileInfo struct {
	info os.FileInfo
}

func NewMiniFileInfo(info os.FileInfo) *MiniFileInfo { return &MiniFileInfo{info: info} }
func (o *MiniFileInfo) GoMiniType() ast.Ident        { return "fs.FileInfo" }
func (o *MiniFileInfo) Name() ast.MiniString         { return ast.NewMiniString(o.info.Name()) }
func (o *MiniFileInfo) Size() ast.MiniInt64          { return ast.NewMiniInt64(o.info.Size()) }
func (o *MiniFileInfo) IsDir() ast.MiniBool          { return ast.NewMiniBool(o.info.IsDir()) }
func (o *MiniFileInfo) ModTime() *MiniTime           { return NewMiniTime(o.info.ModTime()) }

type MiniFsFile struct {
	file afero.File
}

func NewMiniFsFile(file afero.File) *MiniFsFile   { return &MiniFsFile{file: file} }
func (o *MiniFsFile) GoMiniType() ast.Ident       { return "fs.FsFile" }
func (o *MiniFsFile) Read(p []byte) (int, error)  { return o.file.Read(p) }
func (o *MiniFsFile) Write(p []byte) (int, error) { return o.file.Write(p) }
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
func (o *MiniFsFile) Close() error { return o.file.Close() }
func (o *MiniFsFile) Stat() (*MiniFileInfo, error) {
	info, err := o.file.Stat()
	if err != nil {
		return nil, err
	}
	return NewMiniFileInfo(info), nil
}

type MiniFs struct {
	fs afero.Fs
}

func NewMiniFs(fs afero.Fs) *MiniFs     { return &MiniFs{fs: fs} }
func (o *MiniFs) GoMiniType() ast.Ident { return "fs.Fs" }
func (o *MiniFs) Mkdir(path *ast.MiniString, perm *ast.MiniInt64) error {
	return o.fs.Mkdir(path.GoString(), toOsFileMode(perm))
}
func (o *MiniFs) MkdirAll(path *ast.MiniString) error  { return o.fs.MkdirAll(path.GoString(), 0o755) }
func (o *MiniFs) Remove(path *ast.MiniString) error    { return o.fs.RemoveAll(path.GoString()) }
func (o *MiniFs) RemoveAll(path *ast.MiniString) error { return o.fs.RemoveAll(path.GoString()) }
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
	return o.fs.Chmod(path.GoString(), toOsFileMode(mode))
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

func toOsFileMode(n *ast.MiniInt64) os.FileMode {
	if n == nil {
		return 0o644
	}
	return os.FileMode(n.GoValue().(int64))
}

// --- Buffer ---

type MiniBuffer struct {
	buf *bytes.Buffer
}

func NewMiniBuffer() *MiniBuffer { return &MiniBuffer{buf: new(bytes.Buffer)} }
func NewMiniBufferFromBytes(data []byte) *MiniBuffer {
	return &MiniBuffer{buf: bytes.NewBuffer(data)}
}
func (o *MiniBuffer) GoMiniType() ast.Ident       { return "io.Buffer" }
func (o *MiniBuffer) Write(p []byte) (int, error) { return o.buf.Write(p) }
func (o *MiniBuffer) Read(p []byte) (int, error)  { return o.buf.Read(p) }
func (o *MiniBuffer) MiniWrite(p *MiniFile) (ast.MiniInt64, error) {
	n, err := o.buf.Write(p.Bytes())
	return ast.NewMiniInt64(int64(n)), err
}

func (o *MiniBuffer) MiniWriteString(s *ast.MiniString) (ast.MiniInt64, error) {
	n, err := o.buf.WriteString(s.GoString())
	return ast.NewMiniInt64(int64(n)), err
}

func (o *MiniBuffer) MiniRead(n *ast.MiniInt64) (MiniFile, error) {
	p := make([]byte, n.GoValue().(int64))
	read, err := o.buf.Read(p)
	if err != nil && err != io.EOF {
		return MiniFile{}, err
	}
	return NewMiniFile(p[:read]), nil
}
func (o *MiniBuffer) Uint8s() MiniFile       { return NewMiniFile(o.buf.Bytes()) }
func (o *MiniBuffer) String() ast.MiniString { return ast.NewMiniString(o.buf.String()) }
func (o *MiniBuffer) Reset()                 { o.buf.Reset() }
func (o *MiniBuffer) Len() ast.MiniInt64     { return ast.NewMiniInt64(int64(o.buf.Len())) }

// --- Initializers ---

func InitFs(executor *engine.MiniExecutor) {
	executor.AddNativeStruct(ast.PackageStructWrapper{Pkg: "fs", Name: "FileInfo", Stru: (*MiniFileInfo)(nil)})
	executor.AddNativeStruct(ast.PackageStructWrapper{Pkg: "fs", Name: "FsFile", Stru: (*MiniFsFile)(nil)})
	executor.AddNativeStruct(ast.PackageStructWrapper{Pkg: "fs", Name: "Fs", Stru: (*MiniFs)(nil)})

	executor.MustAddPackageFunc("fs", "OS", func() *MiniFs {
		return NewMiniFs(afero.NewOsFs())
	}, "获取操作系统文件系统实例")
	executor.MustAddPackageFunc("fs", "Memory", func() *MiniFs {
		return NewMiniFs(afero.NewMemMapFs())
	}, "获取内存文件系统实例")

	// filepath related
	executor.MustAddPackageFunc("fs", "Join", func(elem []ast.MiniString) ast.MiniString {
		parts := make([]string, len(elem))
		for i, e := range elem {
			parts[i] = e.GoString()
		}
		return ast.NewMiniString(filepath.Join(parts...))
	}, "连接路径片段")
	executor.MustAddPackageFunc("fs", "Abs", func(path *ast.MiniString) (ast.MiniString, error) {
		res, err := filepath.Abs(path.GoString())
		return ast.NewMiniString(res), err
	}, "获取绝对路径")
	executor.MustAddPackageFunc("fs", "Base", func(path *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(filepath.Base(path.GoString()))
	}, "获取路径的最后一个元素")
	executor.MustAddPackageFunc("fs", "Dir", func(path *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(filepath.Dir(path.GoString()))
	}, "获取路径的目录部分")
	executor.MustAddPackageFunc("fs", "Ext", func(path *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(filepath.Ext(path.GoString()))
	}, "获取文件扩展名")
	executor.MustAddPackageFunc("fs", "Clean", func(path *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(filepath.Clean(path.GoString()))
	}, "清理路径")
	executor.MustAddPackageFunc("fs", "IsAbs", func(path *ast.MiniString) ast.MiniBool {
		return ast.NewMiniBool(filepath.IsAbs(path.GoString()))
	}, "判断是否为绝对路径")
}

func InitIO(executor *engine.MiniExecutor) {
	executor.AddNativeStruct(ast.PackageStructWrapper{Pkg: "io", Name: "File", Stru: (*MiniFile)(nil)})
	executor.AddNativeStruct(ast.PackageStructWrapper{Pkg: "io", Name: "NamedFile", Stru: (*MiniNamedFile)(nil)})
	executor.AddNativeStruct(ast.PackageStructWrapper{Pkg: "io", Name: "Buffer", Stru: (*MiniBuffer)(nil)})

	executor.MustAddPackageFunc("io", "ReadAll", func(reader any) (MiniFile, error) {
		r, err := toReader(reader)
		if err != nil {
			return MiniFile{}, err
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return MiniFile{}, err
		}
		return NewMiniFile(data), nil
	}, "读取所有数据直到 EOF")
	executor.MustAddPackageFunc("io", "Copy", func(dst, src any) (ast.MiniInt64, error) {
		w, err := toWriter(dst)
		if err != nil {
			return ast.MiniInt64{}, err
		}
		r, err := toReader(src)
		if err != nil {
			return ast.MiniInt64{}, err
		}
		n, err := io.Copy(w, r)
		return ast.NewMiniInt64(n), err
	}, "将源数据拷贝到目标")
	executor.MustAddPackageFunc("io", "WriteString", func(dst any, s *ast.MiniString) (ast.MiniInt64, error) {
		w, err := toWriter(dst)
		if err != nil {
			return ast.MiniInt64{}, err
		}
		n, err := io.WriteString(w, s.GoString())
		return ast.NewMiniInt64(int64(n)), err
	}, "将字符串写入目标")
	executor.MustAddPackageFunc("io", "NewBuffer", NewMiniBuffer, "创建一个新的字节缓冲区")

	// Helper for creating file from string/base64
	executor.MustAddPackageFunc("io", "NewFile", func(data *ast.MiniString) (MiniFile, error) {
		return NewMiniFile([]byte(data.GoString())), nil
	}, "从字符串创建文件对象")
	executor.MustAddPackageFunc("io", "NewFileFromBase64", func(data *ast.MiniString) (MiniFile, error) {
		goString := data.GoString()
		result, err := base64.URLEncoding.DecodeString(goString)
		if err != nil {
			return MiniFile{}, err
		}
		return NewMiniFile(result), nil
	}, "从 Base64 字符串创建文件对象")
}

func toReader(v any) (io.Reader, error) {
	switch curr := v.(type) {
	case io.Reader:
		return curr, nil
	case *MiniFsFile:
		return curr, nil
	case *MiniBuffer:
		return curr, nil
	default:
		return nil, fmt.Errorf("type %T is not a reader", v)
	}
}

func toWriter(v any) (io.Writer, error) {
	switch curr := v.(type) {
	case io.Writer:
		return curr, nil
	case *MiniFsFile:
		return curr, nil
	case *MiniBuffer:
		return curr, nil
	default:
		return nil, fmt.Errorf("type %T is not a writer", v)
	}
}
