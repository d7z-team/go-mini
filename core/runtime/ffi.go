package runtime

import "gopkg.d7z.net/go-mini/core/ffigo"

type NativeFunc func(e *Executor, session *StackContext, route FFIRoute, args []*Var, argLHS []LHSValue) (*Var, error)

// FFIRoute 存储了外部函数到 Bridge 的映射信息
type FFIRoute struct {
	Bridge   ffigo.FFIBridge
	Native   NativeFunc
	MethodID uint32
	Name     string
	Doc      string
	FuncSig  *RuntimeFuncSig
}
