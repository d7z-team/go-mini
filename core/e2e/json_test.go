package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestJSONLibrary(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
	package main
	import "encoding/json"
	import "fmt"

	func main() {
		// 1. 测试 Marshal
		data := map[string]any{
			"name": "mini",
			"version": 1,
			"alive": true,
			"tags": []any{"script", "engine"},
		}
		
		jsonBytes, err := json.Marshal(data)
		if err != nil { panic("marshal failed: " + err.Error()) }
		fmt.Println("Marshaled JSON:", string(jsonBytes))

		// 2. 测试 Unmarshal
		obj, err1 := json.Unmarshal(jsonBytes)
		if err1 != nil { panic("unmarshal failed: " + err1.Error()) }
		if obj.name != "mini" { panic("unmarshal name mismatch") }
		if obj.version != 1 { panic("unmarshal version mismatch") }
		if obj.alive != true { panic("unmarshal alive mismatch") }
		
		tags := obj.tags
		if len(tags) != 2 { panic("unmarshal tags len mismatch") }
		if tags[0] != "script" { panic("unmarshal tag[0] mismatch") }
		
		fmt.Println("JSON Unmarshal successful")
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
