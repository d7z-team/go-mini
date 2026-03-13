package types

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.d7z.net/go-mini/core/ast"
)

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

func (o *MiniNamedFile) Name() ast.MiniString {
	return ast.NewMiniString(o.name)
}

func (o *MiniNamedFile) GOFileName() string {
	return o.name
}

func (o *MiniNamedFile) String() ast.MiniString {
	return ast.NewMiniString(fmt.Sprintf("File(name=%s, size=%d)", o.name, len(o.file.data)))
}

func (o *MiniNamedFile) OPSType() ast.Ident {
	return "NamedFile"
}

func (o *MiniNamedFile) Bytes() []byte {
	return o.file.data
}

type MiniFile struct {
	data []byte
}

func ReadOsFile(path *ast.MiniString) (*MiniFile, error) {
	data, err := os.ReadFile(path.GoString())
	if err != nil {
		return nil, err
	}
	return &MiniFile{data: data}, nil
}

func WriteOsFile(path *ast.MiniString, file *MiniFile) error {
	dest := path.GoString()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil && !os.IsExist(err) {
		return err
	}
	return os.WriteFile(dest, file.data, 0o644)
}

func NewMiniFile(data []byte) MiniFile {
	return MiniFile{data: data}
}

func (o *MiniFile) OPSType() ast.Ident {
	return "File"
}

func (o *MiniFile) Size() ast.MiniInt64 {
	return ast.NewMiniInt64(int64(len(o.data)))
}

func (o *MiniFile) Bytes() []byte {
	return o.data
}

func (o *MiniFile) ReadString() ast.MiniString {
	return ast.NewMiniString(string(o.data))
}

func (o *MiniFile) GoString() string {
	return string(o.data)
}

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

func NewFileFromPlain(data *ast.MiniString) (MiniFile, error) {
	return NewMiniFile([]byte(data.GoString())), nil
}

func NewFileFromBase64(data *ast.MiniString) (MiniFile, error) {
	goString := data.GoString()
	result, err := base64.URLEncoding.DecodeString(goString)
	if err != nil {
		return MiniFile{}, err
	}
	return NewMiniFile(result), nil
}
