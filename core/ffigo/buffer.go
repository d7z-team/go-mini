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
	buf := bufferPool.Get().(*Buffer)
	buf.buf = buf.buf[:0]
	return buf
}

// ReleaseBuffer 将 Buffer 放回池中
func ReleaseBuffer(b *Buffer) {
	if cap(b.buf) < 65536 {
		bufferPool.Put(b)
	}
}

// Bytes 返回缓冲区的字节切片
func (b *Buffer) Bytes() []byte {
	return b.buf
}

func (b *Buffer) WriteByte(v byte) {
	b.buf = append(b.buf, v)
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

const (
	TypeTagUnknown   byte = 0
	TypeTagInt64     byte = 1
	TypeTagFloat64   byte = 2
	TypeTagString    byte = 3
	TypeTagBytes     byte = 4
	TypeTagBool      byte = 5
	TypeTagHandle    byte = 6
	TypeTagMap       byte = 7
	TypeTagArray     byte = 8
	TypeTagInterface byte = 9
)

func (b *Buffer) WriteInterface(handle uint32, methods map[string]string) {
	b.WriteByte(TypeTagInterface)
	b.WriteUint32(handle)
	b.WriteUint32(uint32(len(methods)))
	for k, v := range methods {
		b.WriteString(k)
		b.WriteString(v)
	}
}

func (b *Buffer) WriteAny(v interface{}) {
	if v == nil {
		b.buf = append(b.buf, TypeTagUnknown)
		return
	}
	switch val := v.(type) {
	case int64:
		b.buf = append(b.buf, TypeTagInt64)
		b.WriteInt64(val)
	case int:
		b.buf = append(b.buf, TypeTagInt64)
		b.WriteInt64(int64(val))
	case uint32:
		b.buf = append(b.buf, TypeTagHandle)
		b.WriteUint32(val)
	case float64:
		b.buf = append(b.buf, TypeTagFloat64)
		b.WriteFloat64(val)
	case string:
		b.buf = append(b.buf, TypeTagString)
		b.WriteString(val)
	case []byte:
		b.buf = append(b.buf, TypeTagBytes)
		b.WriteBytes(val)
	case bool:
		b.buf = append(b.buf, TypeTagBool)
		b.WriteBool(val)
	case map[string]interface{}:
		b.buf = append(b.buf, TypeTagMap)
		b.WriteUint32(uint32(len(val)))
		for k, v := range val {
			b.WriteString(k)
			b.WriteAny(v)
		}
	case []interface{}:
		b.buf = append(b.buf, TypeTagArray)
		b.WriteUint32(uint32(len(val)))
		for _, v := range val {
			b.WriteAny(v)
		}
	default:
		b.buf = append(b.buf, TypeTagUnknown)
	}
}

func (r *Reader) ReadAny() interface{} {
	tag := r.buf[r.offset]
	r.offset++
	switch tag {
	case TypeTagInt64:
		return r.ReadInt64()
	case TypeTagFloat64:
		return r.ReadFloat64()
	case TypeTagString:
		return r.ReadString()
	case TypeTagBytes:
		return r.ReadBytes()
	case TypeTagBool:
		return r.ReadBool()
	case TypeTagHandle:
		return r.ReadUint32()
	case TypeTagMap:
		count := int(r.ReadUint32())
		m := make(map[string]interface{})
		for i := 0; i < count; i++ {
			k := r.ReadString()
			v := r.ReadAny()
			m[k] = v
		}
		return m
	case TypeTagArray:
		count := int(r.ReadUint32())
		a := make([]interface{}, count)
		for i := 0; i < count; i++ {
			a[i] = r.ReadAny()
		}
		return a
	case TypeTagInterface:
		h, m := r.ReadInterface()
		return InterfaceData{Handle: h, Methods: m}
	default:
		return nil
	}
}

type InterfaceData struct {
	Handle  uint32
	Methods map[string]string
}

func (r *Reader) ReadInterface() (uint32, map[string]string) {
	handle := r.ReadUint32()
	count := r.ReadUint32()
	if count > 1024 {
		// 安全限制：一个接口不应有超过 1024 个方法
		return handle, nil
	}
	methods := make(map[string]string)
	for i := uint32(0); i < count; i++ {
		k := r.ReadString()
		v := r.ReadString()
		methods[k] = v
	}
	return handle, methods
}

func NewReader(data []byte) *Reader {
	return &Reader{buf: data, offset: 0}
}

func (r *Reader) ReadByte() byte {
	v := r.buf[r.offset]
	r.offset++
	return v
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
	v := r.buf[r.offset] == 1
	r.offset++
	return v
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

func (r *Reader) Available() int {
	return len(r.buf) - r.offset
}
