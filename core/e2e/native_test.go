package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type NativeMockImpl struct {
	LastValue int64
}

func (m *NativeMockImpl) GetStruct() NativeStruct {
	return NativeStruct{Value: 100, Msg: "copy"}
}

func (m *NativeMockImpl) GetPtr() *NativeStruct {
	return &NativeStruct{Value: 200, Msg: "ptr_copy"}
}

func (m *NativeMockImpl) SetStruct(s NativeStruct) int64 {
	m.LastValue = s.Value
	return s.Value
}

func (m *NativeMockImpl) SetPtr(s *NativeStruct) int64 {
	if s == nil {
		return -1
	}
	m.LastValue = s.Value
	return s.Value
}

func TestNativeObjectInjection(t *testing.T) {
	executor := engine.NewMiniExecutor()
	mock := &NativeMockImpl{}
	registry := ffigo.NewHandleRegistry()

	// 使用生成的注册函数
	RegisterNativeMock(executor, mock, registry)

	code := `
	package main
	import "native"

	type NativeStruct struct {
		Msg   string
		Value int64
	}

	func main() {
		// 1. 获取结构体（值返回，触发全量序列化为 VM 内部 Map）
		s1 := native.GetStruct()
		if s1.Value != 100 { panic("GetStruct value mismatch") }
		if s1.Msg != "copy" { panic("GetStruct msg mismatch") }
		
		// 脚本内修改，采用引用语义（即修改了 s1 变量指向的 Map）
		s1.Value = 150
		
		// 传回 Host（触发序列化回 Go 结构体）
		res1 := native.SetStruct(s1)
		if res1 != 150 { panic("SetStruct failed") }

		// 2. 获取指针（在当前架构下返回的是 opaque Handle）
		s2 := native.GetPtr()
		
		// 注意：目前不支持直接访问 handle.Value，必须通过方法。
		// 这里我们验证它能被原样传回。
		res2 := native.SetPtr(s2)
		if res2 != 200 { panic("SetPtr handle failed") }
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

	if mock.LastValue != 200 {
		t.Errorf("host state mismatch: expected 200, got %d", mock.LastValue)
	}
}
