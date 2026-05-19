package engine_test

import (
	"context"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type nestedRangeOuterLoopBridge struct{}

func (b *nestedRangeOuterLoopBridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)

	switch req.MethodID {
	case 1:
		buf.WriteUvarint(3)
		buf.WriteUvarint(1)
		buf.WriteUvarint(2)
		buf.WriteUvarint(3)
		return buf.Bytes(), nil
	case 2:
		handle := reader.ReadUvarint()
		switch handle {
		case 1, 2:
			buf.WriteVarint(12)
		case 3:
			buf.WriteVarint(9)
		default:
			return nil, fmt.Errorf("unexpected row handle %d", handle)
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
}

func (b *nestedRangeOuterLoopBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, fmt.Errorf("unexpected invoke: %s", req.Method)
}

func (b *nestedRangeOuterLoopBridge) DestroyHandle(handle uint32) error {
	return nil
}

func registerNestedRangeOuterLoopSchemas(exec *engine.MiniExecutor, bridge *nestedRangeOuterLoopBridge) {
	exec.RegisterFFISchema("mock.Rows", bridge, 1,
		runtime.MustParseRuntimeFuncSig("function() Array<HostRef<mock.Row>>"), "")
	exec.RegisterFFISchema("mock.Day", bridge, 2,
		runtime.MustParseRuntimeFuncSig("function(HostRef<mock.Row>) Int64"), "")
}

func TestNestedOuterLoopRangeContinueKeepsOuterLocalAcrossAllLoaders(t *testing.T) {
	const code = `
package main
import "fmt"
import "mock"

var trace = ""

func mark(s string) {
	trace = trace + s + "|"
}

func pair(v Int64) (Int64, Int64) {
	return v, 0
}

func main() {
	rowScan := 0
	nextPage := true
	spin := 1
	for nextPage {
		mark("page-loop")
		for spin > 0 {
			spin = spin - 1
		}
		for _, item := range mock.Rows() {
			rowScan++
			day := mock.Day(item)
			startData, err := pair(1)
			endData, err := pair(9)
			fabuDate, err := pair(day)
			if err != 0 {
				mark("err")
			}
			mark("row-" + string(rowScan))
			if endData < fabuDate {
				mark("continue-" + string(rowScan))
				continue
			}
			if fabuDate < startData {
				nextPage = false
				break
			}
			mark("keep-" + string(rowScan))
			nextPage = false
		}
	}
	fmt.Println("rowScan=", rowScan)
	fmt.Println(trace)
}
`

	for _, loader := range pipelineLoaders(code) {
		t.Run(loader.name, func(t *testing.T) {
			exec := engine.NewMiniExecutor()
			bridge := &nestedRangeOuterLoopBridge{}
			registerNestedRangeOuterLoopSchemas(exec, bridge)

			prog, err := loader.load(exec)
			if err != nil {
				t.Fatalf("load failed: %v", err)
			}
			output := executeWithCapturedOutput(t, prog)
			expected := "rowScan= 3\npage-loop|row-1|continue-1|row-2|continue-2|row-3|keep-3|\n"
			if output != expected {
				t.Fatalf("unexpected output: %q", output)
			}
		})
	}
}
