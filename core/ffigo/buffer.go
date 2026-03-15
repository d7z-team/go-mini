package ffigo

import (
	"encoding/binary"
	"math"
	"sync"
)

// Buffer 是一个用于高性能序列化 FFI 参数的字节缓冲区
type Buffer struct {
	buf []byte
}

var bufferPool = sync.Pool{
	New: func() interface{} {
		return &Buffer{buf: make([]byte, 0, 128)}
	},
}

// GetBuffer 从池中获取一个 Buffer
func GetBuffer() *Buffer {
	b := bufferPool.Get().(*Buffer)
	b.buf = b.buf[:0]
	return b
}

// ReleaseBuffer 将 Buffer 放回池中
func ReleaseBuffer(b *Buffer) {
	bufferPool.Put(b)
}

// Bytes 返回缓冲区的字节切片
func (b *Buffer) Bytes() []byte {
	return b.buf
}

func (b *Buffer) WriteUint32(v uint32) {
	b.buf = binary.LittleEndian.AppendUint32(b.buf, v)
}

func (b *Buffer) WriteInt64(v int64) {
	b.buf = binary.LittleEndian.AppendUint64(b.buf, uint64(v))
}

func (b *Buffer) WriteFloat64(v float64) {
	b.buf = binary.LittleEndian.AppendUint64(b.buf, math.Float64bits(v))
}

func (b *Buffer) WriteBool(v bool) {
	if v {
		b.buf = append(b.buf, 1)
	} else {
		b.buf = append(b.buf, 0)
	}
}

func (b *Buffer) WriteString(v string) {
	b.WriteUint32(uint32(len(v)))
	b.buf = append(b.buf, v...)
}

func (b *Buffer) WriteBytes(v []byte) {
	b.WriteUint32(uint32(len(v)))
	b.buf = append(b.buf, v...)
}

// Reader 是用于从 FFI 参数字节流中读取数据的读取器
type Reader struct {
	buf    []byte
	offset int
}

func NewReader(data []byte) *Reader {
	return &Reader{buf: data, offset: 0}
}

func (r *Reader) ReadUint32() uint32 {
	v := binary.LittleEndian.Uint32(r.buf[r.offset:])
	r.offset += 4
	return v
}

func (r *Reader) ReadInt64() int64 {
	v := binary.LittleEndian.Uint64(r.buf[r.offset:])
	r.offset += 8
	return int64(v)
}

func (r *Reader) ReadFloat64() float64 {
	v := binary.LittleEndian.Uint64(r.buf[r.offset:])
	r.offset += 8
	return math.Float64frombits(v)
}

func (r *Reader) ReadBool() bool {
	v := r.buf[r.offset]
	r.offset += 1
	return v != 0
}

func (r *Reader) ReadString() string {
	l := int(r.ReadUint32())
	v := string(r.buf[r.offset : r.offset+l])
	r.offset += l
	return v
}

func (r *Reader) ReadBytes() []byte {
	l := int(r.ReadUint32())
	// 返回原数组的切片引用，实现零拷贝读取
	v := r.buf[r.offset : r.offset+l]
	r.offset += l
	return v
}
