package runtimes

import (
	"strings"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func InitStrings(executor *engine.MiniExecutor) {
	executor.MustAddPackageFunc("strings", "Contains", func(s, substr *ast.MiniString) ast.MiniBool {
		return ast.NewMiniBool(strings.Contains(s.GoString(), substr.GoString()))
	}, "判断字符串是否包含子串")
	executor.MustAddPackageFunc("strings", "HasPrefix", func(s, prefix *ast.MiniString) ast.MiniBool {
		return ast.NewMiniBool(strings.HasPrefix(s.GoString(), prefix.GoString()))
	}, "判断字符串是否以指定前缀开头")
	executor.MustAddPackageFunc("strings", "HasSuffix", func(s, suffix *ast.MiniString) ast.MiniBool {
		return ast.NewMiniBool(strings.HasSuffix(s.GoString(), suffix.GoString()))
	}, "判断字符串是否以指定后缀结尾")
	executor.MustAddPackageFunc("strings", "Replace", func(s, old, new *ast.MiniString, n *ast.MiniInt64) ast.MiniString {
		res := strings.Replace(s.GoString(), old.GoString(), new.GoString(), int(n.GoValue().(int64)))
		return ast.NewMiniString(res)
	}, "替换指定数量的子串")
	executor.MustAddPackageFunc("strings", "ReplaceAll", func(s, old, new *ast.MiniString) ast.MiniString {
		res := strings.ReplaceAll(s.GoString(), old.GoString(), new.GoString())
		return ast.NewMiniString(res)
	}, "替换所有匹配的子串")
	executor.MustAddPackageFunc("strings", "Split", func(s, sep *ast.MiniString) []ast.MiniString {
		parts := strings.Split(s.GoString(), sep.GoString())
		res := make([]ast.MiniString, len(parts))
		for i, p := range parts {
			res[i] = ast.NewMiniString(p)
		}
		return res
	}, "按分隔符拆分字符串")
	executor.MustAddPackageFunc("strings", "Join", func(elems []ast.MiniString, sep *ast.MiniString) ast.MiniString {
		sElems := make([]string, len(elems))
		for i, e := range elems {
			sElems[i] = e.GoString()
		}
		return ast.NewMiniString(strings.Join(sElems, sep.GoString()))
	}, "将字符串数组连接成一个字符串")
	executor.MustAddPackageFunc("strings", "ToUpper", func(s *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(strings.ToUpper(s.GoString()))
	}, "转换为大写")
	executor.MustAddPackageFunc("strings", "ToLower", func(s *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(strings.ToLower(s.GoString()))
	}, "转换为小写")
	executor.MustAddPackageFunc("strings", "Trim", func(s, cutset *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(strings.Trim(s.GoString(), cutset.GoString()))
	}, "去除字符串两端的指定字符集")
	executor.MustAddPackageFunc("strings", "TrimSpace", func(s *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(strings.TrimSpace(s.GoString()))
	}, "去除字符串两端的空白字符")
	executor.MustAddPackageFunc("strings", "Index", func(s, substr *ast.MiniString) ast.MiniInt64 {
		return ast.NewMiniInt64(int64(strings.Index(s.GoString(), substr.GoString())))
	}, "查找子串在字符串中首次出现的位置")
	executor.MustAddPackageFunc("strings", "Repeat", func(s *ast.MiniString, count *ast.MiniInt64) ast.MiniString {
		return ast.NewMiniString(strings.Repeat(s.GoString(), int(count.GoValue().(int64))))
	}, "重复字符串指定次数")
	executor.MustAddPackageFunc("strings", "Length", func(s *ast.MiniString) ast.MiniInt64 {
		return ast.NewMiniInt64(int64(len(s.GoString())))
	}, "获取字符串长度")
	executor.MustAddPackageFunc("strings", "Count", func(s, substr *ast.MiniString) ast.MiniInt64 {
		return ast.NewMiniInt64(int64(strings.Count(s.GoString(), substr.GoString())))
	}, "统计子串出现的次数")
	executor.MustAddPackageFunc("strings", "EqualFold", func(s, t *ast.MiniString) ast.MiniBool {
		return ast.NewMiniBool(strings.EqualFold(s.GoString(), t.GoString()))
	}, "不区分大小写判断字符串是否相等")
	executor.MustAddPackageFunc("strings", "Fields", func(s *ast.MiniString) []ast.MiniString {
		parts := strings.Fields(s.GoString())
		res := make([]ast.MiniString, len(parts))
		for i, p := range parts {
			res[i] = ast.NewMiniString(p)
		}
		return res
	}, "按空白字符拆分字符串")
	executor.MustAddPackageFunc("strings", "LastIndex", func(s, substr *ast.MiniString) ast.MiniInt64 {
		return ast.NewMiniInt64(int64(strings.LastIndex(s.GoString(), substr.GoString())))
	}, "查找子串在字符串中最后出现的位置")
	executor.MustAddPackageFunc("strings", "TrimPrefix", func(s, prefix *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(strings.TrimPrefix(s.GoString(), prefix.GoString()))
	}, "去除指定前缀")
	executor.MustAddPackageFunc("strings", "TrimSuffix", func(s, suffix *ast.MiniString) ast.MiniString {
		return ast.NewMiniString(strings.TrimSuffix(s.GoString(), suffix.GoString()))
	}, "去除指定后缀")
}
