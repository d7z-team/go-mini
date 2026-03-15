package ffigo

import (
	"bytes"
	"testing"
)

func TestBufferAndReader(t *testing.T) {
	buf := GetBuffer()
	defer ReleaseBuffer(buf)

	buf.WriteUint32(123)
	buf.WriteInt64(-456)
	buf.WriteFloat64(3.14)
	buf.WriteBool(true)
	buf.WriteString("hello")
	buf.WriteBytes([]byte{1, 2, 3})

	reader := NewReader(buf.Bytes())

	if v := reader.ReadUint32(); v != 123 {
		t.Errorf("ReadUint32() = %v, want 123", v)
	}
	if v := reader.ReadInt64(); v != -456 {
		t.Errorf("ReadInt64() = %v, want -456", v)
	}
	if v := reader.ReadFloat64(); v != 3.14 {
		t.Errorf("ReadFloat64() = %v, want 3.14", v)
	}
	if v := reader.ReadBool(); v != true {
		t.Errorf("ReadBool() = %v, want true", v)
	}
	if v := reader.ReadString(); v != "hello" {
		t.Errorf("ReadString() = %v, want hello", v)
	}
	if v := reader.ReadBytes(); !bytes.Equal(v, []byte{1, 2, 3}) {
		t.Errorf("ReadBytes() = %v, want [1 2 3]", v)
	}
}

func TestZeroCopyBytes(t *testing.T) {
	buf := GetBuffer()
	defer ReleaseBuffer(buf)

	data := []byte("large data payload")
	buf.WriteBytes(data)

	reader := NewReader(buf.Bytes())
	readData := reader.ReadBytes()

	// Verify content
	if !bytes.Equal(readData, data) {
		t.Fatal("data mismatch")
	}

	// Verify it's a slice of the original buffer (Zero-copy)
	// We can check if the underlying array pointer is within the buffer's range
	// But simpler: modifying the readData should (if it were shared) modify the buffer.
	// Actually, Reader returns a slice of r.buf.
	readData[0] = 'X'
	if buf.Bytes()[4] != 'X' { // 4 is the offset after Uint32 length
		t.Errorf("Expected zero-copy slice, but modification didn't reflect in buffer")
	}
}
