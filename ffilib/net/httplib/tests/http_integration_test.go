package httplib_test

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("local TCP listen is not permitted in this environment: %v", err)
		}
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestHTTPServerCallbackAndClient(t *testing.T) {
	addr := freeAddr(t)
	testutil.RunCases(t, nil, []testutil.Case{
		{
			Name:    "server-callback-client-and-shutdown",
			Imports: []string{"net/http", "time"},
			Body: fmt.Sprintf(`
srv, err := http.ListenAndServeAsync(%q, func(w *http.ResponseWriter, r *http.Request) {
	http.HeaderSet(http.WriterHeader(w), "X-Mini", http.Method(r))
	http.WriteHeader(w, http.StatusCreated)
	_, writeErr := http.Write(w, []byte("pong:" + http.Path(r)))
	if writeErr != nil {
		panic(writeErr)
	}
})
if err != nil {
	panic(err)
}
time.Sleep(50 * time.Millisecond)
resp, err := http.Get("http://%s/ping")
if err != nil {
	panic(err)
}
test.OutInt(http.StatusCode(resp))
test.Out("|")
body := http.BodyOf(resp)
buf := []byte("................")
n, err := http.ReadBody(body, buf)
if err != nil {
	panic(err)
}
test.OutBytes(buf[:n])
if err = http.CloseBody(body); err != nil {
	panic(err)
}
if err = http.CloseServer(srv); err != nil && err.Error() != "http: Server closed" {
	panic(err)
}
`, addr, addr),
			Want: "201|pong:/ping",
		},
		{
			Name:    "client-facade-get-and-postbytes",
			Imports: []string{"net/http", "time"},
			Body: fmt.Sprintf(`
srv, err := http.ListenAndServeAsync(%q, func(w *http.ResponseWriter, r *http.Request) {
	http.Write(w, []byte(http.Method(r) + ":" + http.Path(r)))
})
if err != nil {
	panic(err)
}
time.Sleep(50 * time.Millisecond)
client := http.DefaultClient()
resp, err := http.ClientGet(client, "http://%s/client")
if err != nil {
	panic(err)
}
body := http.BodyOf(resp)
data := []byte("................")
n, err := http.ReadBody(body, data)
if err != nil {
	panic(err)
}
test.OutBytes(data[:n])
http.CloseBody(body)
test.Out("|")
postResp, err := http.PostBytes("http://%s/post", "text/plain", []byte("payload"))
if err != nil {
	panic(err)
}
postBody := http.BodyOf(postResp)
postData := []byte("................")
postN, err := http.ReadBody(postBody, postData)
if err != nil {
	panic(err)
}
test.OutBytes(postData[:postN])
http.CloseBody(postBody)
http.CloseServer(srv)
`, addr, addr, addr),
			Want: "GET:/client|POST:/post",
		},
	}, testutil.WithSurface(ffilib.Surface()))
}

func TestHTTPListenAndServeYieldsScheduler(t *testing.T) {
	addr := freeAddr(t)
	testutil.RunCases(t, nil, []testutil.Case{
		{
			Name:    "listen-and-serve-yields-scheduler",
			Imports: []string{"net/http", "time"},
			Body: fmt.Sprintf(`
go func() {
	http.ListenAndServe(%q, func(w *http.ResponseWriter, r *http.Request) {
		http.Write(w, []byte("ok"))
	})
}()
time.Sleep(50 * time.Millisecond)
test.Out("after-start|")
resp, err := http.Get("http://%s/")
if err != nil {
	panic(err)
}
body := http.BodyOf(resp)
buf := []byte("..")
n, err := http.ReadBody(body, buf)
if err != nil {
	panic(err)
}
test.OutBytes(buf[:n])
http.CloseBody(body)
`, addr, addr),
			Want: "after-start|ok",
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
