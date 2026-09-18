package console

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStreamsWriteReportsFailure(t *testing.T) {
	streams := &Streams{Stdout: failingWriter{}}
	count, fault, err := streams.Write(context.Background(), StreamStdout, []byte("data"))
	if err != nil || count != 0 || fault.Code != "io" || !strings.Contains(fault.Message, "write failed") {
		t.Fatalf("write = (%d, %#v, %v)", count, fault, err)
	}
}

func TestStreamsReadReportsEOFAndDoesNotCloseInput(t *testing.T) {
	streams := &Streams{Stdin: strings.NewReader("input")}
	data, fault, err := streams.Read(context.Background(), 3)
	if err != nil || fault.Code != "" || string(data) != "inp" {
		t.Fatalf("first read = (%q, %#v, %v)", data, fault, err)
	}
	data, fault, err = streams.Read(context.Background(), 8)
	if err != nil || fault.Code != "" || string(data) != "ut" {
		t.Fatalf("second read = (%q, %#v, %v)", data, fault, err)
	}
	data, fault, err = streams.Read(context.Background(), 8)
	if err != nil || fault.Code != "eof" || len(data) != 0 {
		t.Fatalf("EOF read = (%q, %#v, %v)", data, fault, err)
	}
}

func TestStreamsReadUsesContextAwareReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	data, fault, err := (&Streams{Stdin: contextReader{cancel: cancel}}).Read(ctx, 1)
	if !errors.Is(err, context.Canceled) || len(data) != 0 || fault.Code != "" {
		t.Fatalf("context read = (%q, %#v, %v)", data, fault, err)
	}
}

func FuzzStreamsReadBoundaries(f *testing.F) {
	f.Add("input", int64(3), false)
	f.Add("", int64(0), false)
	f.Add("", int64(64), false)
	f.Add("input", int64(-1), false)
	f.Add("input", int64(1<<20)+1, false)
	f.Add("input", int64(8), true)
	f.Fuzz(func(t *testing.T, input string, size int64, cancel bool) {
		if len(input) > 1<<20 {
			t.Skip()
		}
		ctx := context.Background()
		if cancel {
			cancelled, stop := context.WithCancel(ctx)
			stop()
			ctx = cancelled
		}
		data, fault, err := (&Streams{Stdin: strings.NewReader(input)}).Read(ctx, size)
		if cancel {
			if !errors.Is(err, context.Canceled) || len(data) != 0 || fault.Code != "" {
				t.Fatalf("cancelled read = (%d, %#v, %v)", len(data), fault, err)
			}
			return
		}
		if size < 0 || size > 1<<20 {
			if err != nil || fault.Code != "invalid" || len(data) != 0 {
				t.Fatalf("invalid read = (%d, %#v, %v)", len(data), fault, err)
			}
			return
		}
		if fault.Code == "eof" {
			if err != nil || len(input) != 0 || size == 0 || len(data) != 0 {
				t.Fatalf("EOF read = (%d, %#v, %v), size=%d input=%d", len(data), fault, err, size, len(input))
			}
			return
		}
		if err != nil || fault.Code != "" || int64(len(data)) > size || len(data) > len(input) {
			t.Fatalf("read = (%d, %#v, %v), size=%d input=%d", len(data), fault, err, size, len(input))
		}
	})
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type contextReader struct {
	cancel context.CancelFunc
}

func (contextReader) Read([]byte) (int, error) {
	panic("Read called instead of ReadContext")
}

func (r contextReader) ReadContext(ctx context.Context, _ []byte) (int, error) {
	r.cancel()
	<-ctx.Done()
	return 0, ctx.Err()
}
