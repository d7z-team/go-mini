package dap

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

// OutputReader exposes output appended by the host capabilities used by a DAP
// launch target. Offsets let a session publish each byte exactly once.
type OutputReader interface {
	ReadOutput(stdoutOffset, stderrOffset int) (stdout, stderr string, nextStdout, nextStderr int, err error)
}

// OutputBuffer captures the two host output streams for a DAP launch target.
type OutputBuffer struct {
	mu     sync.Mutex
	stdout bytes.Buffer
	stderr bytes.Buffer
}

type outputWriter struct {
	buffer *OutputBuffer
	stderr bool
}

// NewOutputBuffer creates an empty host output buffer.
func NewOutputBuffer() *OutputBuffer { return &OutputBuffer{} }

// StdoutWriter returns the writer to pass to the stdout host capability.
func (b *OutputBuffer) StdoutWriter() io.Writer { return outputWriter{buffer: b} }

// StderrWriter returns the writer to pass to the stderr host capability.
func (b *OutputBuffer) StderrWriter() io.Writer { return outputWriter{buffer: b, stderr: true} }

func (w outputWriter) Write(data []byte) (int, error) {
	if w.buffer == nil {
		return 0, errors.New("nil DAP output buffer")
	}
	w.buffer.mu.Lock()
	defer w.buffer.mu.Unlock()
	if w.stderr {
		return w.buffer.stderr.Write(data)
	}
	return w.buffer.stdout.Write(data)
}

// ReadOutput returns output appended after the supplied offsets.
func (b *OutputBuffer) ReadOutput(stdoutOffset, stderrOffset int) (stdout, stderr string, nextStdout, nextStderr int, err error) {
	if b == nil {
		return "", "", 0, 0, errors.New("nil DAP output buffer")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	stdoutBytes, stderrBytes := b.stdout.Bytes(), b.stderr.Bytes()
	if stdoutOffset < 0 || stdoutOffset > len(stdoutBytes) || stderrOffset < 0 || stderrOffset > len(stderrBytes) {
		return "", "", 0, 0, errors.New("DAP output offset is out of range")
	}
	return string(stdoutBytes[stdoutOffset:]), string(stderrBytes[stderrOffset:]), len(stdoutBytes), len(stderrBytes), nil
}
