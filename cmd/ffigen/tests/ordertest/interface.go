//go:generate go run ../../../../cmd/ffigen/main.go -pkg ordertest -path gopkg.d7z.net/go-mini/cmd/ffigen/tests/ordertest -out order_ffigen.go interface.go
package ordertest

// ffigen:module order
// ffigen:methods Order

type OrderService interface {
	// New 创建新订单
	New(id string) (*Order, error)

	// AddItem 向订单添加商品
	// 注意：这里使用 *Order 替代 uint32，ffigen 将自动处理句柄映射
	AddItem(o *Order, name string, price float64) error

	// GetTotal 获取总价
	GetTotal(o *Order) (float64, error)

	// Close 关闭订单
	Close(o *Order) error
}
