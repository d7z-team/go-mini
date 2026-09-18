// Package console provides the standard library console host capability.
package console

import (
	"bytes"
	"context"
	"io"
	"sync"
)

const (
	// StreamStdout identifies the standard output stream.
	StreamStdout = 1
	// StreamStderr identifies the standard error stream.
	StreamStderr = 2
)

// Streams connects one guest console session to caller-owned streams. It does
// not close those streams.
type Streams struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	readMu  sync.Mutex
	writeMu sync.Mutex
}

// Write writes one complete payload to stdout or stderr.
func (s *Streams) Write(_ context.Context, stream int64, data []uint8) (int64, FmtStreamFault, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var writer io.Writer
	switch stream {
	case StreamStdout:
		writer = s.Stdout
	case StreamStderr:
		writer = s.Stderr
	default:
		return 0, FmtStreamFault{Code: "invalid", Message: "stream is not writable"}, nil
	}
	if writer == nil {
		return int64(len(data)), FmtStreamFault{}, nil
	}
	n, err := writer.Write(data)
	if err != nil {
		return int64(n), FmtStreamFault{Code: "io", Message: err.Error()}, nil
	}
	if n != len(data) {
		return int64(n), FmtStreamFault{Code: "short_write", Message: io.ErrShortWrite.Error()}, nil
	}
	return int64(n), FmtStreamFault{}, nil
}

// Read reads at most size bytes from stdin. A reader may implement
// ReadContext(context.Context, []byte) to make an in-progress read cancelable.
func (s *Streams) Read(ctx context.Context, size int64) ([]uint8, FmtStreamFault, error) {
	if err := ctx.Err(); err != nil {
		return nil, FmtStreamFault{}, err
	}
	if size < 0 || size > 1<<20 {
		return nil, FmtStreamFault{Code: "invalid", Message: "read size is out of range"}, nil
	}
	if size == 0 {
		return []byte{}, FmtStreamFault{}, nil
	}
	s.readMu.Lock()
	defer s.readMu.Unlock()
	if s.Stdin == nil {
		return nil, FmtStreamFault{Code: "eof", Message: io.EOF.Error()}, nil
	}
	buffer := make([]byte, int(size))
	var n int
	var err error
	if reader, ok := s.Stdin.(interface {
		ReadContext(context.Context, []byte) (int, error)
	}); ok {
		n, err = reader.ReadContext(ctx, buffer)
	} else {
		n, err = s.Stdin.Read(buffer)
	}
	if err != nil && ctx.Err() != nil {
		return nil, FmtStreamFault{}, ctx.Err()
	}
	if err != nil && err != io.EOF {
		return buffer[:n], FmtStreamFault{Code: "io", Message: err.Error()}, nil
	}
	if n == 0 && err == io.EOF {
		return nil, FmtStreamFault{Code: "eof", Message: io.EOF.Error()}, nil
	}
	return buffer[:n], FmtStreamFault{}, nil
}

// Buffer stores console output independently and may be used as an empty
// input stream in deterministic tests.
type Buffer struct {
	Streams
	stdout bytes.Buffer
	stderr bytes.Buffer
}

// NewBuffer creates an in-memory console initialized with input.
func NewBuffer(input string) *Buffer {
	b := &Buffer{}
	b.Streams.Stdin = bytes.NewBufferString(input)
	b.Streams.Stdout = &b.stdout
	b.Streams.Stderr = &b.stderr
	return b
}

// Stdout returns the output written to the standard output stream.
func (b *Buffer) Stdout() string {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return b.stdout.String()
}

// Stderr returns the output written to the standard error stream.
func (b *Buffer) Stderr() string {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return b.stderr.String()
}
