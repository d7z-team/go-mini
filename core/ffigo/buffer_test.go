package ffigo

import (
	"bytes"
	"testing"
)

func TestBufferAndReader(t *testing.T) {
	buf := GetBuffer()
	defer ReleaseBuffer(buf)

	buf.WriteUvarint(123)
	buf.WriteVarint(-456)
	buf.WriteFloat64(3.14)
	buf.WriteBool(true)
	buf.WriteString("hello")
	buf.WriteBytes([]byte{1, 2, 3})

	reader := NewReader(buf.Bytes())

	if v, err := reader.ReadUvarint(); err != nil || v != 123 {
		t.Errorf("ReadUvarint() = %v, want 123", v)
	}
	if v, err := reader.ReadVarint(); err != nil || v != -456 {
		t.Errorf("ReadVarint() = %v, want -456", v)
	}
	if v, err := reader.ReadFloat64(); err != nil || v != 3.14 {
		t.Errorf("ReadFloat64() = %v, want 3.14", v)
	}
	if v, err := reader.ReadBool(); err != nil || v != true {
		t.Errorf("ReadBool() = %v, want true", v)
	}
	if v, err := reader.ReadString(); err != nil || v != "hello" {
		t.Errorf("ReadString() = %v, want hello", v)
	}
	if v, err := reader.ReadBytes(); err != nil || !bytes.Equal(v, []byte{1, 2, 3}) {
		t.Errorf("ReadBytes() = %v, want [1 2 3]", v)
	}
}

func TestZeroCopyBytes(t *testing.T) {
	buf := GetBuffer()
	defer ReleaseBuffer(buf)

	data := []byte("large data payload")
	buf.WriteBytes(data)

	// 获取写入后的原始 buffer
	rawBuf := buf.Bytes()

	reader := NewReader(rawBuf)
	readData, err := reader.ReadBytes()
	if err != nil {
		t.Fatal(err)
	}

	// Verify content
	if !bytes.Equal(readData, data) {
		t.Fatal("data mismatch")
	}

	// 修改读取到的数据
	readData[0] = 'X'

	// 验证原始 buffer 是否未被修改（证明是深拷贝，维护隔离）
	found := false
	for _, b := range rawBuf {
		if b == 'X' {
			found = true
			break
		}
	}
	if found {
		t.Errorf("ReadBytes should return a deep copy to maintain isolation, but modification reflected in buffer")
	}
}

func TestReaderReportsTruncatedPayload(t *testing.T) {
	reader := NewReader([]byte{5, 'a'})
	if got, err := reader.ReadString(); err == nil || got != "" {
		t.Fatalf("ReadString() = %q, want empty string on malformed payload", got)
	}
	if reader.Err() == nil {
		t.Fatal("expected reader error for truncated string")
	}
}

func TestReaderReportsInvalidCollectionCount(t *testing.T) {
	buf := GetBuffer()
	defer ReleaseBuffer(buf)
	_ = buf.WriteByte(TypeTagArray)
	buf.WriteUvarint(MaxWireCollectionItems + 1)

	reader := NewReader(buf.Bytes())
	if got, err := reader.ReadAny(); err == nil || got != nil {
		t.Fatalf("ReadAny() = %#v, want nil for oversized array", got)
	}
	if reader.Err() == nil {
		t.Fatal("expected reader error for oversized array")
	}
}
