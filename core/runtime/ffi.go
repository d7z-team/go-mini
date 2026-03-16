package runtime

import (
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// FFIRoute 存储了外部函数到 Bridge 的映射信息
type FFIRoute struct {
	Bridge   ffigo.FFIBridge
	MethodID uint32
	Returns  string
	Spec     string
}
