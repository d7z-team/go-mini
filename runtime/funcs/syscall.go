package funcs

import (
	"context"
	"encoding/base64"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func InitSyscall(executor *engine.MiniExecutor) {
	executor.MustAddFunc("RandomInt", RandomInt, "生成一个在指定范围内的随机整数")
	executor.MustAddFunc("Sleep", Sleep, "暂停执行指定的时间（如传入毫秒数）")
	executor.MustAddFunc("Base64Dec", Base64Dec, "将 Base64 编码的字符串解码为普通字符串")
	executor.MustAddFunc("Base64Enc", Base64Enc, "将字符串编码为 Base64 格式")
}

// Sleep 暂停执行指定的时间（毫秒）
func Sleep(ctx context.Context, sleep *ast.MiniNumber) error {
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
}

func RandomInt(lt, gt *ast.MiniNumber) ast.MiniNumber {
	var o ast.MiniNumber
	return o.RandomInt(lt, gt)
}

func Base64Dec(dec *ast.MiniString) (ast.MiniString, error) {
	decodeString, err := base64.URLEncoding.DecodeString(dec.GoString())
	if err != nil {
		return ast.MiniString{}, err
	}
	return ast.NewMiniString(string(decodeString)), nil
}

func Base64Enc(dec *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(base64.URLEncoding.EncodeToString([]byte(dec.GoString())))
}
