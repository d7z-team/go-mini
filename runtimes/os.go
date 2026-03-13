package runtimes

import (
	"context"
	"encoding/base64"
	"os"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func InitOS(executor *engine.MiniExecutor) {
	executor.MustAddPackageFunc("os", "Getwd", func() (ast.MiniString, error) {
		dir, err := os.Getwd()
		return ast.NewMiniString(dir), err
	}, "获取当前工作目录")

	// Syscall related functions often registered in global scope
	executor.MustAddFunc("RandomInt", func(lt, gt *ast.MiniInt64) ast.MiniInt64 {
		var o ast.MiniInt64
		return o.RandomInt(lt, gt)
	}, "生成一个在指定范围内的随机整数")
	executor.MustAddFunc("Sleep", func(ctx context.Context, sleep *ast.MiniInt64) error {
		goValue := sleep.GoValue().(int64)
		for i := 0; i < int(goValue/10); i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			time.Sleep(10 * time.Millisecond)
		}
		return nil
	}, "暂停执行指定的时间（如传入毫秒数）")
	executor.MustAddFunc("Base64Dec", func(dec *ast.MiniString) (ast.MiniString, error) {
		decodeString, err := base64.URLEncoding.DecodeString(dec.GoString())
		if err != nil {
			return ast.MiniString{}, err
		}
		return ast.NewMiniString(string(decodeString)), nil
	}, "将 Base64 编码的字符串解码为普通字符串")
	executor.MustAddFunc("Base64Enc", func(dec *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(base64.URLEncoding.EncodeToString([]byte(dec.GoString())))
	}, "将字符串编码为 Base64 格式")
}
