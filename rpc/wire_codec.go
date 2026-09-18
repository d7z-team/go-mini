package rpc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"unicode/utf8"
)

// wireEncoder appends deterministic primitive values to one wire payload.
type wireEncoder struct {
	data []byte
}

// Bytes returns a copy of the encoded payload.
func (e *wireEncoder) Bytes() []byte { return append([]byte(nil), e.data...) }

// Bool appends a boolean value.
func (e *wireEncoder) Bool(value bool) {
	if value {
		e.data = append(e.data, 1)
	} else {
		e.data = append(e.data, 0)
	}
}

// Uint appends an unsigned varint.
func (e *wireEncoder) Uint(value uint64) { e.data = binary.AppendUvarint(e.data, value) }

// Int appends a signed zig-zag varint.
func (e *wireEncoder) Int(value int64) {
	encoded := uint64(value<<1) ^ uint64(value>>63)
	e.Uint(encoded)
}

// Float32 appends an IEEE 754 float in little-endian order.
func (e *wireEncoder) Float32(value float32) {
	e.data = binary.LittleEndian.AppendUint32(e.data, math.Float32bits(value))
}

// Float64 appends an IEEE 754 float in little-endian order.
func (e *wireEncoder) Float64(value float64) {
	e.data = binary.LittleEndian.AppendUint64(e.data, math.Float64bits(value))
}

// Complex64 appends the real and imaginary float32 components.
func (e *wireEncoder) Complex64(value complex64) {
	e.Float32(real(value))
	e.Float32(imag(value))
}

// Complex128 appends the real and imaginary float64 components.
func (e *wireEncoder) Complex128(value complex128) {
	e.Float64(real(value))
	e.Float64(imag(value))
}

// String appends one UTF-8 string as length-delimited bytes.
func (e *wireEncoder) String(value string) {
	e.Uint(uint64(len(value)))
	e.data = append(e.data, value...)
}

// Raw appends one length-delimited byte sequence.
func (e *wireEncoder) Raw(value []byte) {
	e.Uint(uint64(len(value)))
	e.data = append(e.data, value...)
}

// wireDecoder reads primitive values from one immutable wire payload.
type wireDecoder struct {
	data   []byte
	offset int
	limit  int
}

// newWireDecoder prepares immutable data for deterministic bounded decoding.
func newWireDecoder(data []byte, limit int) *wireDecoder {
	return &wireDecoder{data: data, limit: limit}
}

// Remaining returns the number of unread bytes.
func (d *wireDecoder) Remaining() int { return len(d.data) - d.offset }

// Done verifies that the payload has no trailing bytes.
func (d *wireDecoder) Done() error {
	if d.Remaining() != 0 {
		return fmt.Errorf("MRPC payload has %d trailing bytes", d.Remaining())
	}
	return nil
}

func (d *wireDecoder) byte() (byte, error) {
	if d == nil || d.offset >= len(d.data) {
		return 0, errors.New("truncated MRPC payload")
	}
	value := d.data[d.offset]
	d.offset++
	return value, nil
}

// Bool reads one canonical boolean.
func (d *wireDecoder) Bool() (bool, error) {
	value, err := d.byte()
	if err != nil {
		return false, err
	}
	if value > 1 {
		return false, fmt.Errorf("invalid MRPC bool %d", value)
	}
	return value == 1, nil
}

// Uint reads one unsigned varint.
func (d *wireDecoder) Uint() (uint64, error) {
	if d == nil || d.offset >= len(d.data) {
		return 0, errors.New("truncated MRPC payload")
	}
	value, size := binary.Uvarint(d.data[d.offset:])
	if size == 0 {
		return 0, errors.New("truncated MRPC varint")
	}
	if size < 0 {
		return 0, errors.New("overflowing MRPC varint")
	}
	var canonical [binary.MaxVarintLen64]byte
	canonicalSize := binary.PutUvarint(canonical[:], value)
	if canonicalSize != size || !bytes.Equal(d.data[d.offset:d.offset+size], canonical[:canonicalSize]) {
		return 0, errors.New("non-canonical MRPC varint")
	}
	d.offset += size
	return value, nil
}

// Int reads one signed zig-zag varint.
func (d *wireDecoder) Int() (int64, error) {
	value, err := d.Uint()
	if err != nil {
		return 0, err
	}
	return int64(value>>1) ^ -int64(value&1), nil
}

func (d *wireDecoder) fixed(size int) ([]byte, error) {
	if d == nil || size < 0 || size > d.Remaining() {
		return nil, errors.New("truncated MRPC payload")
	}
	value := d.data[d.offset : d.offset+size]
	d.offset += size
	return value, nil
}

// Float32 reads one little-endian IEEE 754 float.
func (d *wireDecoder) Float32() (float32, error) {
	value, err := d.fixed(4)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(value)), nil
}

// Float64 reads one little-endian IEEE 754 float.
func (d *wireDecoder) Float64() (float64, error) {
	value, err := d.fixed(8)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(value)), nil
}

// Complex64 reads real and imaginary float32 components.
func (d *wireDecoder) Complex64() (complex64, error) {
	realPart, err := d.Float32()
	if err != nil {
		return 0, err
	}
	imagPart, err := d.Float32()
	return complex(realPart, imagPart), err
}

// Complex128 reads real and imaginary float64 components.
func (d *wireDecoder) Complex128() (complex128, error) {
	realPart, err := d.Float64()
	if err != nil {
		return 0, err
	}
	imagPart, err := d.Float64()
	return complex(realPart, imagPart), err
}

// Raw reads one bounded length-delimited byte sequence.
func (d *wireDecoder) Raw() ([]byte, error) {
	value, err := d.rawView()
	return append([]byte(nil), value...), err
}

func (d *wireDecoder) rawView() ([]byte, error) {
	size, err := d.Uint()
	if err != nil {
		return nil, err
	}
	if size > uint64(d.limit) || size > uint64(d.Remaining()) {
		return nil, errors.New("invalid MRPC byte length")
	}
	if size == 0 {
		return nil, nil
	}
	value, err := d.fixed(int(size))
	return value, err
}

// String reads and validates one UTF-8 string.
func (d *wireDecoder) String() (string, error) {
	value, err := d.rawView()
	if err != nil {
		return "", err
	}
	if !utf8.Valid(value) {
		return "", errors.New("invalid UTF-8 in MRPC string")
	}
	return string(value), nil
}

func encodeWireLabels(encoder *wireEncoder, labels map[string]string) {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	encoder.Uint(uint64(len(keys)))
	for _, key := range keys {
		encoder.String(key)
		encoder.String(labels[key])
	}
}

func decodeWireLabels(decoder *wireDecoder, limit int) (map[string]string, error) {
	count, err := decoder.Uint()
	if err != nil {
		return nil, err
	}
	if count > uint64(limit) {
		return nil, errors.New("rpc label limit exceeded")
	}
	if count == 0 {
		return nil, nil
	}
	labels := make(map[string]string, int(count))
	for range count {
		key, err := decoder.String()
		if err != nil {
			return nil, err
		}
		value, err := decoder.String()
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, errors.New("rpc label has empty key")
		}
		if _, exists := labels[key]; exists {
			return nil, errors.New("rpc label is duplicated")
		}
		labels[key] = value
	}
	return labels, nil
}

func encodeWireMethod(encoder *wireEncoder, method Method) {
	encoder.String(method.ID)
	encoder.String(method.Service)
	encoder.String(method.Name)
	encoder.String(method.ContractHash)
	encoder.String(method.ResourceTypeHash)
}

func decodeWireMethod(decoder *wireDecoder) (Method, error) {
	method := Method{}
	var err error
	method.ID, err = decoder.String()
	if err == nil {
		method.Service, err = decoder.String()
	}
	if err == nil {
		method.Name, err = decoder.String()
	}
	if err == nil {
		method.ContractHash, err = decoder.String()
	}
	if err == nil {
		method.ResourceTypeHash, err = decoder.String()
	}
	return method, err
}

func encodeWireResource(encoder *wireEncoder, ref *ResourceRef) {
	encoder.Bool(ref != nil)
	if ref == nil {
		return
	}
	encoder.Uint(ref.Epoch)
	encoder.Uint(ref.ObjectID)
	encoder.String(ref.TypeHash)
}

func decodeWireResource(decoder *wireDecoder) (*ResourceRef, error) {
	present, err := decoder.Bool()
	if err != nil || !present {
		return nil, err
	}
	ref := &ResourceRef{}
	ref.Epoch, err = decoder.Uint()
	if err == nil {
		ref.ObjectID, err = decoder.Uint()
	}
	if err == nil {
		ref.TypeHash, err = decoder.String()
	}
	return ref, err
}
