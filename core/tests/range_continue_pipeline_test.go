package engine_test

import (
	"context"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

type rangeContinueHandleBridge struct {
	downloaded []uint64
}

func (b *rangeContinueHandleBridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)

	switch req.MethodID {
	case 1:
		buf.WriteUvarint(2)
		buf.WriteUvarint(1)
		buf.WriteUvarint(2)
		return buf.Bytes(), nil
	case 2:
		handle := reader.ReadUvarint()
		switch handle {
		case 1:
			buf.WriteString("skip")
		case 2:
			buf.WriteString("hit")
		default:
			return nil, fmt.Errorf("unexpected Published handle %d", handle)
		}
		return buf.Bytes(), nil
	case 3:
		label := reader.ReadString()
		switch label {
		case "skip":
			buf.WriteVarint(30)
		case "hit":
			buf.WriteVarint(5)
		default:
			return nil, fmt.Errorf("unexpected Day label %q", label)
		}
		return buf.Bytes(), nil
	case 4:
		handle := reader.ReadUvarint()
		if handle == 1 {
			panic("skip row reached Download after continue")
		}
		b.downloaded = append(b.downloaded, handle)
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
}

func (b *rangeContinueHandleBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, fmt.Errorf("unexpected invoke: %s", req.Method)
}

func (b *rangeContinueHandleBridge) DestroyHandle(handle uint32) error {
	return nil
}

func registerRangeContinueHandleSchemas(t *testing.T, exec *engine.MiniExecutor, bridge *rangeContinueHandleBridge) {
	t.Helper()
	testsurface.UseRoutes(t, exec, bridge,
		testsurface.Route("mock.Rows", 1, runtime.MustParseRuntimeFuncSig("function() Array<HostRef<mock.Row>>"), ""),
		testsurface.Route("mock.Published", 2, runtime.MustParseRuntimeFuncSig("function(HostRef<mock.Row>) String"), ""),
		testsurface.Route("mock.Day", 3, runtime.MustParseRuntimeFuncSig("function(String) Int64"), ""),
		testsurface.Route("mock.Download", 4, runtime.MustParseRuntimeFuncSig("function(HostRef<mock.Row>) Void"), ""),
	)
}

func TestRangeContinueSkipsFFITailAcrossAllLoaders(t *testing.T) {
	const code = `
package main
import "mock"

var trace = ""

func mark(s string) {
	trace = trace + s + "|"
}

func pair(v Int64) (Int64, Int64) {
	return v, 0
}

func main() {
	for _, item := range mock.Rows() {
		published := mock.Published(item)
		startData, err := pair(1)
		endData, err := pair(20)
		fabuDate, err := pair(mock.Day(published))
		if err != 0 {
			mark("err")
		}
		mark("parsed-" + published)
		if endData < fabuDate {
			mark("continue-" + published)
			continue
		}
		if fabuDate < startData {
			break
		}
		mark("tail-" + published)
		if true {
			mark("download-before-" + published)
			mock.Download(item)
			mark("download-after-" + published)
		}
	}
}
`

	for _, loader := range pipelineLoaders(code) {
		t.Run(loader.name, func(t *testing.T) {
			exec := engine.NewMiniExecutor()
			bridge := &rangeContinueHandleBridge{}
			registerRangeContinueHandleSchemas(t, exec, bridge)

			prog, err := loader.load(exec)
			if err != nil {
				t.Fatalf("load failed: %v", err)
			}
			snapshot := executeAndSnapshot(t, prog)
			trace, ok := snapshot.LoadGlobal("trace")
			if !ok || trace == nil || trace.Str != "parsed-skip|continue-skip|parsed-hit|tail-hit|download-before-hit|download-after-hit|" {
				t.Fatalf("unexpected trace: %#v", trace)
			}
			if len(bridge.downloaded) != 1 || bridge.downloaded[0] != 2 {
				t.Fatalf("unexpected downloaded handles: %#v", bridge.downloaded)
			}
		})
	}
}
