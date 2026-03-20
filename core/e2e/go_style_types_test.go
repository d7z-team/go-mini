package e2e

import (
	"context"
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
				// new(int) 在隔离语义下返回的是一个持有 0 的变量
                // 注意：目前 go-mini 不支持 *p 这种解引用语法，但 Ptr<T> 在成员访问时是透明的
				ps := new(Point)
				if ps.X != 0 { panic("new struct failed") }
                ps.X = 100
                if ps.X != 100 { panic("struct assignment failed") }
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := executor.NewRuntimeByGoCode(tt.code)
			if tt.name == "InvalidMakeType" {
				if err == nil || (!strings.Contains(err.Error(), "非法类型") && !strings.Contains(err.Error(), "make: 非法类型")) {
					t.Fatalf("Expected compile error for invalid make type, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			// 调试打印：验证转换后的 AST 是否为规范化形式
			// astJSON, _ := prog.MarshalIndentJSON("", "  ")
			// t.Logf("Normalized AST: %s", string(astJSON))

			err = prog.Execute(context.Background())
			if err != nil {
				t.Errorf("Execute failed: %v", err)
			}
		})
	}
}
