package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestReverseProxy(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	var calc Any

	func main() {
		c := make(map[String]Any)
		c["Add"] = func(a, b Int64) Int64 { return a + b }
		c["Format"] = func(prefix String, val Int64) String { 
			return prefix + ": " + String(val) 
		}
		c["Divide"] = func(a, b Int64) (Int64, String) {
			if b == 0 { return 0, "division by zero" }
			return a / b, ""
		}
		calc = c
	}
	`
	runtimeObj, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	// 1. 执行脚本初始化全局变量
	err = runtimeObj.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session := runtimeObj.LastSession()
	res, _ := session.Load("calc")

	// 2. 创建反向代理
	proxy := NewScriptCalculator_ReverseProxy(runtimeObj, session, res, nil)

	// 3. 调用并验证
	t.Run("Add", func(t *testing.T) {
		if got := proxy.Add(10, 20); got != 30 {
			t.Errorf("Add(10, 20) = %d, want 30", got)
		}
	})

	t.Run("Format", func(t *testing.T) {
		if got := proxy.Format("Result", 123); got != "Result: 123" {
			t.Errorf("Format = %q, want %q", got, "Result: 123")
		}
	})

	t.Run("Divide", func(t *testing.T) {
		v, err := proxy.Divide(10, 2)
		if err != nil || v != 5 {
			t.Errorf("Divide(10, 2) = (%d, %v), want (5, nil)", v, err)
		}

		_, err = proxy.Divide(10, 0)

		if err == nil || err.Error() != "division by zero" {
			t.Errorf("Divide(10, 0) error = %v, want 'division by zero'", err)
		}
	})
}
