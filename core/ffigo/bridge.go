package ffigo

import "context"

// FFIBridge 是 VM 和 Host 通信的唯一物理通道
type FFIBridge interface {
	// Call 接收方法路由 ID 和序列化后的参数字节流，返回结果字节流
	Call(ctx context.Context, methodID uint32, args []byte) (ret []byte, err error)
	// Invoke 接收方法名和序列化后的参数（用于动态/接口调用）
	Invoke(ctx context.Context, method string, args []byte) (ret []byte, err error)
	// DestroyHandle 释放由 Host 创建的句柄
	DestroyHandle(handle uint32) error
}
