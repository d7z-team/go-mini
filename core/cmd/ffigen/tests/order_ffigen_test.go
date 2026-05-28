package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/cmd/ffigen/tests/ordertest"
)

func TestOrderFFIGen(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	impl := ordertest.NewOrderImpl()
	if err := executor.UseSurface(ordertest.SurfaceOrderService(impl)); err != nil {
		t.Fatal(err)
	}
	code := `
	package main
	import "order"

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
