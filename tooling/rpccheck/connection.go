package rpccheck

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
)

// MessageConn adds only a bounded length prefix to physical Endpoint fragments.
// A single Endpoint reader and writer own the respective halves of Conn.
type MessageConn struct{ Conn net.Conn }

func (c MessageConn) Read(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.Conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
	}
	var size [4]byte
	if _, err := io.ReadFull(c.Conn, size[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n > 256 {
		return nil, errors.New("peer fragment exceeds limit")
	}
	data := make([]byte, n)
	_, err := io.ReadFull(c.Conn, data)
	return data, err
}

func (c MessageConn) Write(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) > 256 {
		return errors.New("peer fragment exceeds limit")
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.Conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(data)))
	_, err := (&net.Buffers{size[:], data}).WriteTo(c.Conn)
	return err
}
func (c MessageConn) Close() error { return c.Conn.Close() }
