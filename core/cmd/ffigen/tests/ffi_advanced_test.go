package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestAdvancedFFIExecution(t *testing.T) {
	obj := &TestObj{Name: "Shared"}
	impl := &AdvancedFFIImpl{obj: obj}

	executor := engine.NewMiniExecutor()
	if err := executor.UseSurface(SurfaceAdvancedFFI(impl)); err != nil {
		t.Fatal(err)
	}

	code := `
		package main
		import "test"

		func main() {
			// 1. 验证句柄去重 (Identity)
			obj1 := test.GetSameObject()
			obj2 := test.GetSameObject()
			if !test.IsSame(obj1, obj2) {
				panic("Handle identity failed")
			}

			// 2. 验证 Map 键 (Bool, Float64)
			m := make(map[bool]string)
			m[true] = "true"
			m[false] = "false"
			
			resMap := test.EchoMap(m)
			if !resMap[1.5] {
				panic("Map key/value logic failed")
			}

			// 3. 验证嵌入结构体 (Embedded)
			e := test.EmbeddedStruct{BaseField: "from_vm", ExtraField: 42}
			
			resE := test.EchoEmbedded(e)
			if resE.BaseField != "from_vm" || resE.ExtraField != 42 {
			        panic("Embedded struct flattening failed")
			}		}
	`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}
}
