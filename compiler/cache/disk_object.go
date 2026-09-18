//go:build !minigo

package cache

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"time"
)

const (
	entryVersion      = "v2"
	entrySize         = 154
	objectRaw         = byte(0)
	objectGzip        = byte(1)
	compressLimit     = 1024
	objectSizeBytes   = 8
	maxDiskOutputSize = 1 << 30
)

var objectMagic = []byte("mini-go-cache-object-v2\x00")

func decodeEntry(id ActionID, data []byte) (Entry, error) {
	if len(data) != entrySize || string(data[:3]) != entryVersion+" " || data[67] != ' ' || data[132] != ' ' || data[153] != '\n' {
		return Entry{}, errors.New("invalid cache action entry")
	}
	if string(data[3:67]) != id.String() {
		return Entry{}, errors.New("cache action identifier mismatch")
	}
	output, err := ParseOutputID(string(data[68:132]))
	if err != nil {
		return Entry{}, err
	}
	size, err := strconv.ParseInt(string(data[133:153]), 10, 64)
	if err != nil || size < 0 {
		return Entry{}, errors.New("invalid cache output size")
	}
	return Entry{Output: output, Size: size}, nil
}

func encodeDiskOutput(data []byte) ([]byte, error) {
	if len(data) > maxDiskOutputSize {
		return nil, errors.New("cache output size limit exceeded")
	}
	codec := objectRaw
	payload := data
	if len(data) >= compressLimit {
		var compressed bytes.Buffer
		writer, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
		if err != nil {
			return nil, err
		}
		writer.Header.ModTime = time.Time{}
		writer.Header.OS = 255
		if _, err := writer.Write(data); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		if compressed.Len() < len(data) {
			codec = objectGzip
			payload = compressed.Bytes()
		}
	}
	encoded := make([]byte, 0, len(objectMagic)+1+objectSizeBytes+len(payload))
	encoded = append(encoded, objectMagic...)
	encoded = append(encoded, codec)
	var size [objectSizeBytes]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(data)))
	encoded = append(encoded, size[:]...)
	encoded = append(encoded, payload...)
	return encoded, nil
}

func decodeDiskOutput(data []byte) ([]byte, error) {
	headerSize := len(objectMagic) + 1 + objectSizeBytes
	if len(data) < headerSize || !bytes.Equal(data[:len(objectMagic)], objectMagic) {
		return nil, errors.New("unsupported cache object format")
	}
	expected := binary.BigEndian.Uint64(data[len(objectMagic)+1 : headerSize])
	if expected > maxDiskOutputSize {
		return nil, errors.New("cache output size limit exceeded")
	}
	payload := data[headerSize:]
	switch data[len(objectMagic)] {
	case objectRaw:
		if uint64(len(payload)) != expected {
			return nil, errors.New("cache output length mismatch")
		}
		return append([]byte(nil), payload...), nil
	case objectGzip:
		compressed := bytes.NewReader(payload)
		reader, err := gzip.NewReader(compressed)
		if err != nil {
			return nil, err
		}
		reader.Multistream(false)
		decoded, readErr := io.ReadAll(io.LimitReader(reader, int64(expected)+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if uint64(len(decoded)) != expected {
			return nil, errors.New("cache output length mismatch")
		}
		if compressed.Len() != 0 {
			return nil, errors.New("cache output has trailing data")
		}
		return decoded, nil
	default:
		return nil, errors.New("unsupported cache object codec")
	}
}
