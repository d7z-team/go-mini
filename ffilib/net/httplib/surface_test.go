package httplib

import (
	"errors"
	"io"
	"net"
	gohttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestResponseWriterHeaderAddAndDel(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := &responseWriter{w: recorder, live: true}

	writer.HeaderSet("X-Test", "one")
	writer.HeaderAdd("X-Test", "two")
	if got := recorder.Header().Values("X-Test"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("unexpected added header values: %#v", got)
	}

	writer.HeaderDel("X-Test")
	if got := recorder.Header().Values("X-Test"); len(got) != 0 {
		t.Fatalf("expected deleted header, got %#v", got)
	}
}

func TestEncodeBytesIntErrorPreservesEOF(t *testing.T) {
	payload := encodeBytesIntError([]byte("abc"), 0, io.EOF)
	r := ffigo.NewReader(payload)
	count, err := r.ReadCount(ffigo.MaxWireCollectionItems, "copy-back")
	if err != nil {
		t.Fatalf("ReadCount failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("copy-back count = %d, want 1", count)
	}
	data, err := r.ReadBytes()
	if err != nil {
		t.Fatalf("ReadBytes failed: %v", err)
	}
	if string(data) != "abc" {
		t.Fatalf("unexpected copy-back bytes: %q", data)
	}
	n, err := r.ReadVarint()
	if err != nil {
		t.Fatalf("ReadVarint failed: %v", err)
	}
	if n != 0 {
		t.Fatalf("read count = %d, want 0", n)
	}
	errData, err := r.ReadRawError()
	if err != nil {
		t.Fatalf("ReadRawError failed: %v", err)
	}
	if errData.Message != io.EOF.Error() {
		t.Fatalf("error message = %q, want %q", errData.Message, io.EOF.Error())
	}
}

func TestHTTPServerStartReportsListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("local TCP listen is not permitted in this environment: %v", err)
		}
		t.Fatal(err)
	}
	defer ln.Close()

	conflict := &httpServer{srv: &gohttp.Server{Addr: ln.Addr().String(), Handler: gohttp.NewServeMux()}}
	err = conflict.start()
	if err == nil {
		conflict.Close()
		t.Fatal("expected listen conflict error")
	}
	if !strings.Contains(err.Error(), "address already in use") && !errors.Is(err, gohttp.ErrServerClosed) {
		t.Fatalf("unexpected listen conflict error: %v", err)
	}
}
