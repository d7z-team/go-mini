package oshost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"testing"

	"github.com/d7z-team/mini-go/rpc"
	rpcrouter "github.com/d7z-team/mini-go/rpc/router"
)

func TestFilesystemUnsupportedFaultIdentity(t *testing.T) {
	code, message := operationError(fmt.Errorf("operation: %w", errors.ErrUnsupported))
	if code != "unsupported" || message != "operation: unsupported operation" {
		t.Fatalf("unsupported fault = %q, %q", code, message)
	}
}

func TestFileReadDirNonpositiveCountReadsAll(t *testing.T) {
	for _, count := range []int64{0, -1, -2, -1 << 63} {
		filesystem, err := NewMemoryFilesystem(map[string][]byte{"a": {}, "b": {}})
		if err != nil {
			t.Fatal(err)
		}
		file, err := filesystem.Open(".", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		binding := &fileBinding{file: file}
		entries, fault, err := binding.ReadDir(context.Background(), count)
		if err != nil || fault.Code != "" || len(entries) != 2 {
			t.Fatalf("ReadDir(%d) = %v, %#v, %v", count, entries, fault, err)
		}
		if err := binding.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFilesystemPositionalIO(t *testing.T) {
	memory, err := NewMemoryFilesystem(nil)
	if err != nil {
		t.Fatal(err)
	}
	rooted, err := NewRootedFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rooted.Close()
	for _, test := range []struct {
		name       string
		filesystem Filesystem
	}{
		{"memory", memory}, {"rooted", rooted},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := test.filesystem.Open("data", os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if n, err := file.Write([]byte("abcdef")); err != nil || n != 6 {
				t.Fatalf("write = %d, %v", n, err)
			}
			if _, err := file.Seek(1, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 4)
			if n, err := file.ReadAt(buf, 4); n != 2 || !errors.Is(err, io.EOF) || string(buf[:n]) != "ef" {
				t.Fatalf("readat = %q, %d, %v", buf, n, err)
			}
			if n, err := file.WriteAt([]byte("XY"), 2); err != nil || n != 2 {
				t.Fatalf("writeat = %d, %v", n, err)
			}
			if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 1 {
				t.Fatalf("cursor = %d, %v", offset, err)
			}
			if n, err := file.ReadAt(nil, 100); n != 0 || err != nil {
				t.Fatalf("empty readat = %d, %v", n, err)
			}
			if _, err := file.ReadAt(buf, -1); err == nil {
				t.Fatal("negative read offset accepted")
			}
			if _, err := file.WriteAt(buf, -1); err == nil {
				t.Fatal("negative write offset accepted")
			}
			appended, err := test.filesystem.Open("data", os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := appended.WriteAt([]byte("x"), 0); err == nil {
				t.Fatal("positional write on append file accepted")
			}
			if err := appended.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := file.ReadAt(buf, 0); !errors.Is(err, fs.ErrClosed) {
				t.Fatalf("read closed file = %v", err)
			}
		})
	}
}

func TestProviderRequiresBackend(t *testing.T) {
	if _, err := NewFilesystemProvider(nil); err == nil {
		t.Fatal("nil filesystem accepted")
	}
	if _, err := NewEnvironmentProvider(nil); err == nil {
		t.Fatal("nil environment accepted")
	}
}

func TestRootedFilesystemOperationsAfterClose(t *testing.T) {
	filesystem, err := NewRootedFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := filesystem.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := filesystem.Stat("."); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("Stat after Close = %v, want fs.ErrClosed", err)
	}
	if err := filesystem.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
}

func TestFilesystemRPCBindingTransfersResourceLifecycle(t *testing.T) {
	ctx := context.Background()
	filesystem, err := NewMemoryFilesystem(map[string][]byte{"input.txt": []byte("input")})
	if err != nil {
		t.Fatal(err)
	}
	filesystemProvider, err := NewFilesystemProvider(filesystem)
	if err != nil {
		t.Fatal(err)
	}
	gateway := rpcrouter.New(rpcrouter.Options{})
	publication, err := gateway.Publish([]rpcrouter.ProviderEntry{{Provider: filesystemProvider}})
	if err != nil {
		t.Fatal(err)
	}
	defer publication.ForceClose(context.Background())
	filesystemClient, err := BindOsFilesystemClient(ctx, gateway, rpc.BindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer filesystemClient.Close()
	missing, missingFault, missingErr := filesystemClient.Open(ctx, "missing.txt", 0, 0)
	if missingErr != nil || missing != nil || missingFault.Code != "not_exist" {
		t.Fatalf("missing file = (%#v, %#v, %v)", missing, missingFault, missingErr)
	}
	file, osFault, err := filesystemClient.Open(ctx, "input.txt", 0, 0)
	if err != nil || osFault.Code != "" || file == nil {
		t.Fatalf("typed open = (%#v, %#v, %v)", file, osFault, err)
	}
	data, osFault, err := file.Read(ctx, 16)
	if err != nil || osFault.Code != "" || string(data) != "input" {
		t.Fatalf("typed read = (%q, %#v, %v)", data, osFault, err)
	}
	if err := file.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(ctx); err != nil {
		t.Fatalf("resource close is not idempotent: %v", err)
	}
}

func TestMemoryFilesystemSupportsFileLifecycle(t *testing.T) {
	filesystem, err := NewMemoryFilesystem(map[string][]byte{"input.txt": []byte("input")})
	if err != nil {
		t.Fatal(err)
	}
	file, err := filesystem.Open("input.txt", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("+value")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := filesystem.Open("input.txt", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(opened)
	if err != nil || string(data) != "input+value" {
		t.Fatalf("read = %q, err=%v", data, err)
	}
	_ = opened.Close()
}

func TestRootedFilesystemRejectsEscape(t *testing.T) {
	filesystem, err := NewRootedFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer filesystem.Close()
	if _, err := filesystem.Open("../outside", 0, 0); err == nil {
		t.Fatal("rooted filesystem accepted parent traversal")
	}
}
