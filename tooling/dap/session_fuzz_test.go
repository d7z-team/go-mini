package dap

import (
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	protocol "github.com/google/go-dap"
)

func FuzzDAPCodec(f *testing.F) {
	f.Add([]byte(`{"seq":1,"type":"request","command":"threads"}`))
	f.Add([]byte(`{"seq":1,"type":"request","command":"launch","arguments":{}}`))
	codec := protocol.NewCodec()
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _ = codec.DecodeMessage(data)
	})
}

func FuzzDAPPagination(f *testing.F) {
	f.Add(0, 0, 0)
	f.Add(1, 10, 100)
	f.Add(-1, 1, 3)
	f.Add(100, 1, 3)
	f.Fuzz(func(t *testing.T, start, count, rawLength int) {
		length := int(uint(rawLength) % 4097)
		first, last := page(start, count, length)

		wantFirst := start
		if wantFirst < 0 {
			wantFirst = 0
		}
		if wantFirst > length {
			wantFirst = length
		}
		wantLast := length
		if count > 0 && count < length-wantFirst {
			wantLast = wantFirst + count
		}
		if first != wantFirst || last != wantLast {
			t.Fatalf("page(%d, %d, %d) = (%d, %d), want (%d, %d)", start, count, length, first, last, wantFirst, wantLast)
		}
		if first < 0 || first > last || last > length {
			t.Fatalf("invalid page bounds (%d, %d) for length %d", first, last, length)
		}
	})
}

func FuzzDAPSourceIdentity(f *testing.F) {
	f.Add("main.mgo", false)
	f.Add("pkg/main.mgo", true)
	f.Add("../outside.mgo", false)
	f.Add("file:///outside/main.mgo", true)
	f.Fuzz(func(t *testing.T, input string, useURI bool) {
		if len(input) > 4096 {
			return
		}
		session := NewSession(strings.NewReader(""), io.Discard, nil)
		session.target = LaunchTarget{RootPath: "/workspace", ModulePath: "fuzz"}
		path := input
		if useURI {
			session.pathFormat = "uri"
			if !strings.HasPrefix(path, "file:") {
				path = (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(session.target.RootPath, path))}).String()
			}
		}

		modulePath, file, err := session.sourceIdentity(protocol.Source{Path: path})
		if err != nil {
			return
		}
		if modulePath != "fuzz" && !strings.HasPrefix(modulePath, "fuzz/") {
			t.Fatalf("module path %q escaped root", modulePath)
		}
		if filepath.IsAbs(file) || file == ".." || strings.HasPrefix(file, "../") {
			t.Fatalf("source path %q escaped root", file)
		}

		absolute := filepath.Join(session.target.RootPath, filepath.FromSlash(file))
		roundModule, roundFile, err := session.sourceIdentity(protocol.Source{Path: session.clientPath(absolute)})
		if err != nil || roundModule != modulePath || roundFile != file {
			t.Fatalf("source identity round trip = %q, %q, %v; want %q, %q", roundModule, roundFile, err, modulePath, file)
		}
	})
}
