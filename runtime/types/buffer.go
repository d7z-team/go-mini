package types

import (
	"bytes"
	"io"

	"gopkg.d7z.net/go-mini/core/ast"
)

type MiniBuffer struct {
	buf *bytes.Buffer
}

func NewMiniBuffer() *MiniBuffer {
	return &MiniBuffer{buf: new(bytes.Buffer)}
}

func NewMiniBufferFromBytes(data []byte) *MiniBuffer {
	return &MiniBuffer{buf: bytes.NewBuffer(data)}
}

func (o *MiniBuffer) OPSType() ast.Ident {
	return "Buffer"
}

func (o *MiniBuffer) Write(p []byte) (n int, err error) {
	return o.buf.Write(p)
}

func (o *MiniBuffer) Read(p []byte) (n int, err error) {
	return o.buf.Read(p)
}

func (o *MiniBuffer) MiniWrite(p *MiniFile) (ast.MiniNumber, error) {
	n, err := o.buf.Write(p.Bytes())
	return ast.NewMiniNumber(int64(n)), err
}

func (o *MiniBuffer) MiniWriteString(s *ast.MiniString) (ast.MiniNumber, error) {
	n, err := o.buf.WriteString(s.GoString())
	return ast.NewMiniNumber(int64(n)), err
}

func (o *MiniBuffer) MiniRead(n *ast.MiniNumber) (MiniFile, error) {
	p := make([]byte, n.GoValue().(int64))
	read, err := o.buf.Read(p)
	if err != nil && err != io.EOF {
		return MiniFile{}, err
	}
	return NewMiniFile(p[:read]), nil
}

func (o *MiniBuffer) Bytes() MiniFile {
	return NewMiniFile(o.buf.Bytes())
}

func (o *MiniBuffer) String() ast.MiniString {
	return ast.NewMiniString(o.buf.String())
}

func (o *MiniBuffer) Reset() {
	o.buf.Reset()
}

func (o *MiniBuffer) Len() ast.MiniNumber {
	return ast.NewMiniNumber(int64(o.buf.Len()))
}
