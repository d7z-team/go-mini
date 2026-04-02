package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestGoStyleTypes(t *testing.T) {
	executor := engine.NewMiniExecutor()

	tests := []struct {
		name string
		code string
	}{
		{
			name: "MakeSlice",
			code: `
			package main
			func main() {
				s := make([]int, 5)
				if len(s) != 5 { panic("len failed") }
				s[0] = 100
				if s[0] != 100 { panic("value failed") }
			}`,
		},
		{
			name: "MakeMap",
			code: `
			package main
			func main() {
				m := make(map[string]int)
				m["key"] = 42
				if m["key"] != 42 { panic("map failed") }
			}`,
		},
		{
			name: "SliceLiteral",
			code: `
			package main
			func main() {
				s := []int{1, 2, 3}
				if len(s) != 3 { panic("len failed") }
				if s[1] != 2 { panic("value failed") }
				
				s2 := append(s, 4)
				if len(s2) != 4 || s2[3] != 4 { panic("append failed") }
			}`,
		},
		{
			name: "MapLiteral",
			code: `
			package main
			func main() {
				m := map[string]int{"a": 1, "b": 2}
				if m["a"] != 1 || m["b"] != 2 { panic("map lit failed") }
				
				delete(m, "a")
				if m["a"] != 0 { panic("delete failed") } // VM 默认返回零值
			}`,
		},
		{
			name: "NestedContainers",
			code: `
			package main
			func main() {
				// 嵌套切片
				ss := [][]int{{1}, {2, 3}}
				if len(ss) != 2 || len(ss[1]) != 2 { panic("nested slice len failed") }
				if ss[1][1] != 3 { panic("nested value failed") }

				// 嵌套 Map
				mm := map[string]map[string]int{
					"outer": {"inner": 99},
				}
				if mm["outer"]["inner"] != 99 { panic("nested map failed") }
			}`,
		},
		{
			name: "ComplexMake",
			code: `
			package main
			func main() {
				// 测试更复杂的类型名解析
				s := make([]map[string][]int, 1)
				s[0] = make(map[string][]int)
				s[0]["test"] = []int{10, 20}
				if s[0]["test"][1] != 20 { panic("complex make failed") }
			}`,
		},
		{
			name: "EmptyMapHeuristic",
			code: `
			package main
			func main() {
				m := map[string]int{}
				if len(m) != 0 { panic("empty map len failed") }
				m["a"] = 1
				if m["a"] != 1 { panic("empty map assignment failed") }
			}`,
		},
		{
			name: "MapZeroValueTyped",
			code: `
			package main
			func main() {
				m := map[string]string{"exists": "hello"}
				if m["missing"] != "" { panic("string zero value failed") }
				
				mb := map[string]bool{"exists": true}
				if mb["missing"] != false { panic("bool zero value failed") }
			}`,
		},
		{
			name: "InvalidMakeType",
			code: `
			package main
            type SomethingRandom struct {}
			func main() {
				// 这是一个非法的类型（目前 make 不支持自定义 struct），应该在编译期报错
				m := make("SomethingRandom")
			}`,
		},
		{
			name: "NewBuiltin",
			code: `
			package main
			type Point struct { X int; Y int }
			func main() {
				// new(int) 在隔离语义下直接返回零值
				ps := new(Point)
				if ps.X != 0 { panic("new struct failed") }
                ps.X = 100
                if ps.X != 100 { panic("struct assignment failed") }
                
                // 验证引用语义：赋值后应该指向同一个对象
                ps2 := ps
                ps2.Y = 200
                if ps.Y != 200 { panic("struct reference failed") }
			}`,
		},
		{
			name: "NilComparison",
			code: `
			package main
			func main() {
				var m map[string]int
				if m != nil { panic("uninitialized map should be nil") }
                
                s := ""
                if s != "" { panic("empty string comparison failed") }
                // 在 go-mini 中，空字符串不等于 nil
                if s == nil { panic("empty string should not be nil") }
                
                if nil != nil { panic("nil equality failed") }
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "InvalidMakeType" {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("expected panic for invalid make type")
					} else if !strings.Contains(fmt.Sprint(r), "第一个参数必须是类型") {
						t.Errorf("unexpected panic message: %v", r)
					}
				}()
			}
			prog, err := executor.NewRuntimeByGoCode(tt.code)
			if tt.name == "InvalidMakeType" {
				return
			}
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			// astJSON, _ := prog.MarshalIndentJSON("", "  ")
			// t.Logf("Normalized AST: %s", string(astJSON))

			err = prog.Execute(context.Background())
			if err != nil {
				t.Errorf("Execute failed: %v", err)
			}
		})
	}
}
