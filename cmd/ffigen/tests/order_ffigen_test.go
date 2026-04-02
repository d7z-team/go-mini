package tests

import (
	"context"
	"testing"

	"gopkg.d7z.net/go-mini/cmd/ffigen/tests/ordertest"
	engine "gopkg.d7z.net/go-mini/core"
)

func TestOrderFFIGen(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()
	impl := ordertest.NewOrderImpl()
	ordertest.RegisterOrderService(executor, impl, executor.HandleRegistry())
	code := `
	package main
	import "order"
	import "fmt"

	func main() {
		// 1. 创建订单
		o, err := order.New("ORD-FFIGEN")
		if err != nil {
			panic("New order failed: " + err.Error())
		}
		
		// 3. 添加商品
		o.AddItem("Apple Vision Pro", 3499.0)
		o.AddItem("MacBook Pro", 1999.0)
		
		// 4. 计算总价
		total, err1 := o.GetTotal()
		if err1 != nil { panic(err1.Error()) }
		if total != 5498.0 {
			panic("total mismatch")
		}
		
		// 5. 关闭并验证
		o.Close()
		
		// 6. 尝试在关闭后添加
		err2 := o.AddItem("Broken", 1.0)
		if err2 == nil {
			panic("should have caught error for closed order")
		}
		
		fmt.Println("Complex Business Object Verified, Total:", total)
	}
	`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
