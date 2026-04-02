package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestReverseProxy(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	type ReversePoint struct {
		X Int64
		Y Int64
	}

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
		c["Log"] = func(msg String) String {
			return "ctx:" + msg
		}
		c["Join"] = func(prefix String, values ...String) String {
			out := prefix
			for _, v := range values {
				out = out + "|" + v
			}
			return out
		}
		c["AcceptPoint"] = func(p ReversePoint) Int64 {
			return p.X + p.Y
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

	t.Run("ContextParamIgnoredByProxyMarshaling", func(t *testing.T) {
		if got := proxy.Log(context.Background(), "hello"); got != "ctx:hello" {
			t.Fatalf("Log = %q, want %q", got, "ctx:hello")
		}
	})

	t.Run("VariadicArgumentsUseSliceInReverseProxy", func(t *testing.T) {
		if got := proxy.Join("root", []string{"a", "b", "c"}); got != "root|a|b|c" {
			t.Fatalf("Join = %q, want %q", got, "root|a|b|c")
		}
	})

	t.Run("StructArgument", func(t *testing.T) {
		if got := proxy.AcceptPoint(ReversePoint{X: 10, Y: 20}); got != 30 {
			t.Fatalf("AcceptPoint = %d, want 30", got)
		}
	})
}

func TestReverseProxyMissingMethodAndNilCallable(t *testing.T) {
	executor := engine.NewMiniExecutor()
	runtimeObj, err := executor.NewRuntimeByGoCode(`
	package main
	var calc Any
	func main() {
		c := make(map[String]Any)
		c["Add"] = func(a, b Int64) Int64 { return a + b }
		calc = c
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeObj.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	session := runtimeObj.LastSession()
	callable, err := session.Load("calc")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("MissingMethodReturnsZeroValues", func(t *testing.T) {
		proxy := NewScriptCalculator_ReverseProxy(runtimeObj, session, callable, nil)
		if got := proxy.Format("x", 1); got != "" {
			t.Fatalf("Format zero fallback = %q, want empty string", got)
		}
		if got := proxy.Log(context.Background(), "hello"); got != "" {
			t.Fatalf("Log zero fallback = %q, want empty string", got)
		}
		if got := proxy.Join("root", []string{"a", "b"}); got != "" {
			t.Fatalf("Join zero fallback = %q, want empty string", got)
		}
		if got := proxy.AcceptPoint(ReversePoint{X: 1, Y: 2}); got != 0 {
			t.Fatalf("AcceptPoint zero fallback = %d, want 0", got)
		}
		if value, err := proxy.Divide(10, 1); value != 0 {
			t.Fatalf("Divide zero fallback value = %d, want 0", value)
		} else if err == nil || !strings.Contains(err.Error(), "Divide not found") {
			t.Fatalf("Divide missing-method error = %v, want method-not-found", err)
		}
	})

	t.Run("NilCallableReturnsZeroValues", func(t *testing.T) {
		proxy := NewScriptCalculator_ReverseProxy(runtimeObj, session, nil, nil)
		if got := proxy.Add(1, 2); got != 0 {
			t.Fatalf("Add zero fallback = %d, want 0", got)
		}
	})
}

func TestReverseProxyErrorHandleRoundTrip(t *testing.T) {
	executor := engine.NewMiniExecutor()
	runtimeObj, err := executor.NewRuntimeByGoCode(`
	package main
	var calc Any
	func main() {
		c := make(map[String]Any)
		c["Divide"] = func(a, b Int64) (Int64, Error) {
			panic("boom")
		}
		calc = c
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeObj.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	session := runtimeObj.LastSession()
	callable, err := session.Load("calc")
	if err != nil {
		t.Fatal(err)
	}

	proxy := NewScriptCalculator_ReverseProxy(runtimeObj, session, callable, nil)
	_, divErr := proxy.Divide(10, 2)
	if divErr == nil {
		t.Fatal("expected reverse proxy error")
	}
	if vmErr, ok := divErr.(*runtime.VMError); ok {
		if vmErr.Message == "" {
			t.Fatalf("expected vm error message, got %#v", vmErr)
		}
	} else if divErr.Error() == "" {
		t.Fatalf("expected non-empty error, got %#v", divErr)
	} else if !strings.Contains(divErr.Error(), "boom") {
		t.Fatalf("expected reverse proxy error to contain panic message, got %#v", divErr)
	}
}
