package ffigo

// FFIBridge 是 VM 和 Host 通信的唯一物理通道
type FFIBridge interface {
	// Call 接收方法路由 ID 和序列化后的参数字节流，返回结果字节流
	Call(methodID uint32, args []byte) (ret []byte, err error)
}
