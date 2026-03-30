package ffigo

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
)

// Wire Format Constants

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
	TypeTagError     byte = 10
	TypeTagStruct    byte = 11
	TypeTagPointer   byte = 12
)

// Buffer - Raw & Tagged Serializer

type Buffer struct {
	buf []byte
}

var bufferPool = sync.Pool{
	New: func() interface{} {
		return &Buffer{buf: make([]byte, 0, 512)}
	},
}

func GetBuffer() *Buffer {
	b := bufferPool.Get().(*Buffer)
	b.buf = b.buf[:0]
	return b
}

func ReleaseBuffer(b *Buffer) {
	if cap(b.buf) < 65536 {
		bufferPool.Put(b)
	}
}

func (b *Buffer) Bytes() []byte { return b.buf }
func (b *Buffer) Len() int      { return len(b.buf) }

func (b *Buffer) WriteByte(v byte) error {
	b.buf = append(b.buf, v)
	return nil
}

func (b *Buffer) WriteUvarint(v uint64) {
	b.buf = binary.AppendUvarint(b.buf, v)
}

func (b *Buffer) WriteVarint(v int64) {
	b.buf = binary.AppendVarint(b.buf, v)
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
	b.WriteUvarint(uint64(len(v)))
	b.buf = append(b.buf, v...)
}

func (b *Buffer) WriteBytes(v []byte) {
	b.WriteUvarint(uint64(len(v)))
	b.buf = append(b.buf, v...)
}

func (b *Buffer) WriteRawError(msg string, handle uint32) {
	b.WriteString(msg)
	b.WriteUvarint(uint64(handle))
}

func (b *Buffer) WriteRawInterface(handle uint32, methods map[string]string) {
	b.WriteUvarint(uint64(handle))
	b.WriteUvarint(uint64(len(methods)))
	for k, v := range methods {
		b.WriteString(k)
		b.WriteString(v)
	}
}

func (b *Buffer) WriteAny(v interface{}) {
	if v == nil {
		b.WriteByte(TypeTagUnknown)
		return
	}
	switch val := v.(type) {
	case int64:
		b.WriteByte(TypeTagInt64)
		b.WriteVarint(val)
	case int:
		b.WriteByte(TypeTagInt64)
		b.WriteVarint(int64(val))
	case float64:
		b.WriteByte(TypeTagFloat64)
		b.WriteFloat64(val)
	case string:
		b.WriteByte(TypeTagString)
		b.WriteString(val)
	case []byte:
		b.WriteByte(TypeTagBytes)
		b.WriteBytes(val)
	case bool:
		b.WriteByte(TypeTagBool)
		b.WriteBool(val)
	case uint32:
		b.WriteByte(TypeTagHandle)
		b.WriteUvarint(uint64(val))
	case map[string]interface{}:
		b.WriteByte(TypeTagMap)
		b.WriteUvarint(uint64(len(val)))
		for k, v := range val {
			b.WriteString(k)
			b.WriteAny(v)
		}
	case []interface{}:
		b.WriteByte(TypeTagArray)
		b.WriteUvarint(uint64(len(val)))
		for _, v := range val {
			b.WriteAny(v)
		}
	case ErrorData:
		b.WriteByte(TypeTagError)
		b.WriteRawError(val.Message, val.Handle)
	case InterfaceData:
		b.WriteByte(TypeTagInterface)
		b.WriteRawInterface(val.Handle, val.Methods)
	case error:
		b.WriteByte(TypeTagError)
		b.WriteRawError(val.Error(), 0)
	default:
		b.WriteByte(TypeTagUnknown)
	}
}

// Reader - High-performance Decoupled Deserializer

type Reader struct {
	buf    []byte
	offset int
}

func NewReader(data []byte) *Reader {
	return &Reader{buf: data, offset: 0}
}

func (r *Reader) Available() int { return len(r.buf) - r.offset }

func (r *Reader) ReadByte() (byte, error) {
	if r.offset >= len(r.buf) {
		return 0, io.EOF
	}
	v := r.buf[r.offset]
	r.offset++
	return v, nil
}

func (r *Reader) ReadUvarint() uint64 {
	v, n := binary.Uvarint(r.buf[r.offset:])
	r.offset += n
	return v
}

func (r *Reader) ReadVarint() int64 {
	v, n := binary.Varint(r.buf[r.offset:])
	r.offset += n
	return v
}

func (r *Reader) ReadFloat64() float64 {
	v := binary.LittleEndian.Uint64(r.buf[r.offset:])
	r.offset += 8
	return math.Float64frombits(v)
}

func (r *Reader) ReadBool() bool {
	v, _ := r.ReadByte()
	return v != 0
}

func (r *Reader) ReadString() string {
	l := int(r.ReadUvarint())
	v := string(r.buf[r.offset : r.offset+l])
	r.offset += l
	return v
}

func (r *Reader) ReadBytes() []byte {
	l := int(r.ReadUvarint())
	if l == 0 {
		return nil
	}
	v := make([]byte, l)
	copy(v, r.buf[r.offset:r.offset+l])
	r.offset += l
	return v
}

func (r *Reader) ReadRawError() ErrorData {
	msg := r.ReadString()
	handle := uint32(r.ReadUvarint())
	return ErrorData{Message: msg, Handle: handle}
}

func (r *Reader) ReadRawInterface() InterfaceData {
	handle := uint32(r.ReadUvarint())
	count := int(r.ReadUvarint())
	if count > 1024 {
		count = 1024
	}
	methods := make(map[string]string)
	for i := 0; i < count; i++ {
		k := r.ReadString()
		v := r.ReadString()
		methods[k] = v
	}
	return InterfaceData{Handle: handle, Methods: methods}
}

func (r *Reader) ReadAny() interface{} {
	if r.Available() == 0 {
		return nil
	}
	tag, _ := r.ReadByte()
	switch tag {
	case TypeTagInt64:
		return r.ReadVarint()
	case TypeTagFloat64:
		return r.ReadFloat64()
	case TypeTagString:
		return r.ReadString()
	case TypeTagBytes:
		return r.ReadBytes()
	case TypeTagBool:
		return r.ReadBool()
	case TypeTagHandle:
		return uint32(r.ReadUvarint())
	case TypeTagMap:
		count := int(r.ReadUvarint())
		m := make(map[string]interface{})
		for i := 0; i < count; i++ {
			k := r.ReadString()
			v := r.ReadAny()
			m[k] = v
		}
		return m
	case TypeTagArray:
		count := int(r.ReadUvarint())
		a := make([]interface{}, count)
		for i := 0; i < count; i++ {
			a[i] = r.ReadAny()
		}
		return a
	case TypeTagInterface:
		return r.ReadRawInterface()
	case TypeTagError:
		return r.ReadRawError()
	case TypeTagStruct:
		count := int(r.ReadUvarint())
		fields := make([]StructField, count)
		for i := 0; i < count; i++ {
			fields[i].Name = r.ReadString()
			fields[i].Value = r.ReadAny()
		}
		return &VMStruct{Fields: fields}
	case TypeTagPointer:
		return &VMPointer{Value: r.ReadAny()}
	default:
		return nil
	}
}

// Core Data Structures

type StructField struct {
	Name  string
	Value interface{}
}

type VMStruct struct {
	Fields []StructField
}

func (s *VMStruct) String() string {
	var buf strings.Builder
	buf.WriteString("{")
	for i, f := range s.Fields {
		if i > 0 {
			buf.WriteString(" ")
		}
		buf.WriteString(f.Name)
		buf.WriteString(":")
		fmt.Fprintf(&buf, "%v", f.Value)
	}
	buf.WriteString("}")
	return buf.String()
}

type VMPointer struct {
	Value interface{}
}

func (p *VMPointer) String() string {
	return "&" + fmt.Sprintf("%v", p.Value)
}

type InterfaceData struct {
	Handle  uint32
	Methods map[string]string
}

type ErrorData struct {
	Message string
	Handle  uint32
}

func (e ErrorData) Error() string { return e.Message }
