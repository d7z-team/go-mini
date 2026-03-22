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

	if v := reader.ReadUvarint(); v != 123 {
		t.Errorf("ReadUvarint() = %v, want 123", v)
	}
	if v := reader.ReadVarint(); v != -456 {
		t.Errorf("ReadVarint() = %v, want -456", v)
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

	// 获取写入后的原始 buffer
	rawBuf := buf.Bytes()

	reader := NewReader(rawBuf)
	readData := reader.ReadBytes()

	// Verify content
	if !bytes.Equal(readData, data) {
		t.Fatal("data mismatch")
	}

	// 修改读取到的数据
	readData[0] = 'X'

	// 验证原始 buffer 是否也被修改（证明是零拷贝切片）
	// 注意：由于使用了 Varint，数据的起始位置取决于长度字段的编码长度
	found := false
	for _, b := range rawBuf {
		if b == 'X' {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected zero-copy slice, but modification didn't reflect in buffer")
	}
}
