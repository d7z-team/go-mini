package ffigo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
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
	TypeTagMap       byte = 7
	TypeTagArray     byte = 8
	TypeTagInterface byte = 9
	TypeTagError     byte = 10
	TypeTagCallback  byte = 11
)

const maxAnyInt64 = uint64(1<<63 - 1)

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

func (b *Buffer) WriteRawCallback(handle uint32, sig string) {
	b.WriteUvarint(uint64(handle))
	b.WriteString(sig)
}

func (b *Buffer) writeAnyInt64(v int64) {
	_ = b.WriteByte(TypeTagInt64)
	b.WriteVarint(v)
}

func (b *Buffer) writeAnyUint64(v uint64) error {
	if v > maxAnyInt64 {
		return fmt.Errorf("uint64 value %d exceeds FFI Any int64 range", v)
	}
	b.writeAnyInt64(int64(v))
	return nil
}

func (b *Buffer) WriteAny(v interface{}) error {
	return b.writeAny(v, "Any")
}

func (b *Buffer) writeAny(v interface{}, path string) error {
	if v == nil {
		_ = b.WriteByte(TypeTagUnknown)
		return nil
	}
	switch val := v.(type) {
	case int64:
		b.writeAnyInt64(val)
	case int:
		b.writeAnyInt64(int64(val))
	case int8:
		b.writeAnyInt64(int64(val))
	case int16:
		b.writeAnyInt64(int64(val))
	case int32:
		b.writeAnyInt64(int64(val))
	case uint:
		return b.writeAnyUint64(uint64(val))
	case uint8:
		b.writeAnyInt64(int64(val))
	case uint16:
		b.writeAnyInt64(int64(val))
	case uint32:
		b.writeAnyInt64(int64(val))
	case uint64:
		return b.writeAnyUint64(val)
	case float64:
		_ = b.WriteByte(TypeTagFloat64)
		b.WriteFloat64(val)
	case float32:
		_ = b.WriteByte(TypeTagFloat64)
		b.WriteFloat64(float64(val))
	case string:
		_ = b.WriteByte(TypeTagString)
		b.WriteString(val)
	case []byte:
		_ = b.WriteByte(TypeTagBytes)
		b.WriteBytes(val)
	case bool:
		_ = b.WriteByte(TypeTagBool)
		b.WriteBool(val)
	case map[string]interface{}:
		_ = b.WriteByte(TypeTagMap)
		b.WriteUvarint(uint64(len(val)))
		for k, v := range val {
			b.WriteString(k)
			if err := b.writeAny(v, fmt.Sprintf("%s[%q]", path, k)); err != nil {
				return err
			}
		}
	case []interface{}:
		_ = b.WriteByte(TypeTagArray)
		b.WriteUvarint(uint64(len(val)))
		for i, v := range val {
			if err := b.writeAny(v, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case ErrorData:
		if val.Handle != 0 {
			return fmt.Errorf("%s cannot carry host error handle", path)
		}
		_ = b.WriteByte(TypeTagError)
		b.WriteRawError(val.Message, 0)
	case InterfaceData:
		return fmt.Errorf("%s cannot carry interface", path)
	case CallbackData:
		return fmt.Errorf("%s cannot carry callback", path)
	case error:
		_ = b.WriteByte(TypeTagError)
		b.WriteRawError(val.Error(), 0)
	default:
		return fmt.Errorf("%s cannot carry %T", path, v)
	}
	return nil
}

// Reader - High-performance Decoupled Deserializer

type Reader struct {
	buf    []byte
	offset int
	err    error
}

func NewReader(data []byte) *Reader {
	return &Reader{buf: data, offset: 0}
}

const (
	MaxWireBlobBytes       = 64 << 20
	MaxWireCollectionItems = 1 << 20
	MaxWireInterfaceItems  = 1024
)

func (r *Reader) Available() int {
	if r == nil || r.offset >= len(r.buf) {
		return 0
	}
	return len(r.buf) - r.offset
}

func (r *Reader) Err() error {
	if r == nil {
		return io.ErrUnexpectedEOF
	}
	return r.err
}

func (r *Reader) setErr(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *Reader) ReadByte() (byte, error) {
	if r == nil {
		return 0, io.ErrUnexpectedEOF
	}
	if r.err != nil {
		return 0, r.err
	}
	if r.offset >= len(r.buf) {
		r.setErr(io.ErrUnexpectedEOF)
		return 0, r.err
	}
	v := r.buf[r.offset]
	r.offset++
	return v, nil
}

func (r *Reader) ReadUvarint() (uint64, error) {
	if r == nil || r.err != nil {
		return 0, r.Err()
	}
	v, n := binary.Uvarint(r.buf[r.offset:])
	if n == 0 {
		r.setErr(io.ErrUnexpectedEOF)
		return 0, r.err
	}
	if n < 0 {
		r.setErr(errors.New("wire uvarint overflow"))
		return 0, r.err
	}
	r.offset += n
	return v, nil
}

func (r *Reader) ReadVarint() (int64, error) {
	if r == nil || r.err != nil {
		return 0, r.Err()
	}
	v, n := binary.Varint(r.buf[r.offset:])
	if n == 0 {
		r.setErr(io.ErrUnexpectedEOF)
		return 0, r.err
	}
	if n < 0 {
		r.setErr(errors.New("wire varint overflow"))
		return 0, r.err
	}
	r.offset += n
	return v, nil
}

func (r *Reader) ReadFloat64() (float64, error) {
	if r == nil || r.err != nil {
		return 0, r.Err()
	}
	if r.Available() < 8 {
		r.setErr(io.ErrUnexpectedEOF)
		return 0, r.err
	}
	v := binary.LittleEndian.Uint64(r.buf[r.offset:])
	r.offset += 8
	return math.Float64frombits(v), nil
}

func (r *Reader) ReadBool() (bool, error) {
	v, err := r.ReadByte()
	return v != 0, err
}

func (r *Reader) readByteLength(limit int, label string) (int, error) {
	raw, err := r.ReadUvarint()
	if err != nil {
		return 0, err
	}
	if raw > uint64(limit) {
		r.setErr(fmt.Errorf("wire %s length %d exceeds limit %d", label, raw, limit))
		return 0, r.err
	}
	l := int(raw)
	if l > r.Available() {
		r.setErr(io.ErrUnexpectedEOF)
		return 0, r.err
	}
	return l, nil
}

func (r *Reader) ReadCount(limit int, label string) (int, error) {
	raw, err := r.ReadUvarint()
	if err != nil {
		return 0, err
	}
	if raw > uint64(limit) {
		r.setErr(fmt.Errorf("wire %s count %d exceeds limit %d", label, raw, limit))
		return 0, r.err
	}
	return int(raw), nil
}

func (r *Reader) ReadString() (string, error) {
	l, err := r.readByteLength(MaxWireBlobBytes, "string")
	if err != nil {
		return "", err
	}
	v := string(r.buf[r.offset : r.offset+l])
	r.offset += l
	return v, nil
}

func (r *Reader) ReadBytes() ([]byte, error) {
	l, err := r.readByteLength(MaxWireBlobBytes, "bytes")
	if err != nil {
		return nil, err
	}
	if l == 0 {
		return nil, nil
	}
	v := make([]byte, l)
	copy(v, r.buf[r.offset:r.offset+l])
	r.offset += l
	return v, nil
}

func (r *Reader) ReadRawError() (ErrorData, error) {
	msg, err := r.ReadString()
	if err != nil {
		return ErrorData{}, err
	}
	handle, err := r.ReadUvarint()
	if err != nil {
		return ErrorData{}, err
	}
	return ErrorData{Message: msg, Handle: uint32(handle)}, nil
}

func (r *Reader) ReadRawInterface() (InterfaceData, error) {
	rawHandle, err := r.ReadUvarint()
	if err != nil {
		return InterfaceData{}, err
	}
	handle := uint32(rawHandle)
	count, err := r.ReadCount(MaxWireInterfaceItems, "interface method")
	if err != nil {
		return InterfaceData{Handle: handle}, err
	}
	methods := make(map[string]string)
	for i := 0; i < count; i++ {
		k, err := r.ReadString()
		if err != nil {
			return InterfaceData{Handle: handle}, err
		}
		v, err := r.ReadString()
		if err != nil {
			return InterfaceData{Handle: handle}, err
		}
		methods[k] = v
	}
	return InterfaceData{Handle: handle, Methods: methods}, nil
}

func (r *Reader) ReadRawCallback() (CallbackData, error) {
	rawHandle, err := r.ReadUvarint()
	if err != nil {
		return CallbackData{}, err
	}
	sig, err := r.ReadString()
	if err != nil {
		return CallbackData{Handle: uint32(rawHandle)}, err
	}
	return CallbackData{Handle: uint32(rawHandle), Signature: sig}, nil
}

func (r *Reader) ReadAny() (interface{}, error) {
	if r == nil || r.err != nil || r.Available() == 0 {
		return nil, r.Err()
	}
	tag, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
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
	case TypeTagMap:
		count, err := r.ReadCount(MaxWireCollectionItems, "map")
		if err != nil {
			return nil, err
		}
		m := make(map[string]interface{}, count)
		for i := 0; i < count; i++ {
			k, err := r.ReadString()
			if err != nil {
				return nil, err
			}
			v, err := r.ReadAny()
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
		return m, nil
	case TypeTagArray:
		count, err := r.ReadCount(MaxWireCollectionItems, "array")
		if err != nil {
			return nil, err
		}
		a := make([]interface{}, count)
		for i := 0; i < count; i++ {
			a[i], err = r.ReadAny()
			if err != nil {
				return nil, err
			}
		}
		return a, nil
	case TypeTagInterface:
		r.err = errors.New("FFI Any cannot carry interface")
		return nil, r.err
	case TypeTagCallback:
		r.err = errors.New("FFI Any cannot carry callback")
		return nil, r.err
	case TypeTagError:
		data, err := r.ReadRawError()
		if err != nil {
			return nil, err
		}
		if data.Handle != 0 {
			r.err = errors.New("FFI Any cannot carry host error handle")
			return nil, r.err
		}
		return data, nil
	default:
		return nil, nil
	}
}

// Core Data Structures

type InterfaceData struct {
	Handle  uint32
	Methods map[string]string
}

type CallbackData struct {
	Handle    uint32
	Signature string
}

type ErrorData struct {
	Message string
	Handle  uint32
}

func (e ErrorData) Error() string { return e.Message }
