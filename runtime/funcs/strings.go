package funcs

import (
	"strings"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func InitStrings(executor *engine.MiniExecutor) {
	executor.MustAddPackageFunc("strings", "Contains", StrContains, "判断字符串是否包含子串")
	executor.MustAddPackageFunc("strings", "HasPrefix", StrHasPrefix, "判断字符串是否以指定前缀开头")
	executor.MustAddPackageFunc("strings", "HasSuffix", StrHasSuffix, "判断字符串是否以指定后缀结尾")
	executor.MustAddPackageFunc("strings", "Replace", StrReplace, "替换指定数量的子串")
	executor.MustAddPackageFunc("strings", "ReplaceAll", StrReplaceAll, "替换所有匹配的子串")
	executor.MustAddPackageFunc("strings", "Split", StrSplit, "按分隔符拆分字符串")
	executor.MustAddPackageFunc("strings", "Join", StrJoin, "将字符串数组连接成一个字符串")
	executor.MustAddPackageFunc("strings", "ToUpper", StrToUpper, "转换为大写")
	executor.MustAddPackageFunc("strings", "ToLower", StrToLower, "转换为小写")
	executor.MustAddPackageFunc("strings", "Trim", StrTrim, "去除字符串两端的指定字符集")
	executor.MustAddPackageFunc("strings", "TrimSpace", StrTrimSpace, "去除字符串两端的空白字符")
	executor.MustAddPackageFunc("strings", "Index", StrIndex, "查找子串在字符串中首次出现的位置")
	executor.MustAddPackageFunc("strings", "Repeat", StrRepeat, "重复字符串指定次数")
	executor.MustAddPackageFunc("strings", "Length", StrLength, "获取字符串长度")
	executor.MustAddPackageFunc("strings", "Count", StrCount, "统计子串出现的次数")
	executor.MustAddPackageFunc("strings", "EqualFold", StrEqualFold, "不区分大小写判断字符串是否相等")
	executor.MustAddPackageFunc("strings", "Fields", StrFields, "按空白字符拆分字符串")
	executor.MustAddPackageFunc("strings", "LastIndex", StrLastIndex, "查找子串在字符串中最后出现的位置")
	executor.MustAddPackageFunc("strings", "TrimPrefix", StrTrimPrefix, "去除指定前缀")
	executor.MustAddPackageFunc("strings", "TrimSuffix", StrTrimSuffix, "去除指定后缀")
}

func StrContains(s, substr *ast.MiniString) ast.MiniBool {
	return ast.NewMiniBool(strings.Contains(s.GoString(), substr.GoString()))
}

func StrHasPrefix(s, prefix *ast.MiniString) ast.MiniBool {
	return ast.NewMiniBool(strings.HasPrefix(s.GoString(), prefix.GoString()))
}

func StrHasSuffix(s, suffix *ast.MiniString) ast.MiniBool {
	return ast.NewMiniBool(strings.HasSuffix(s.GoString(), suffix.GoString()))
}

func StrReplace(s, old, new *ast.MiniString, n *ast.MiniNumber) ast.MiniString {
	res := strings.Replace(s.GoString(), old.GoString(), new.GoString(), int(n.GoValue().(int64)))
	return ast.NewMiniString(res)
}

func StrReplaceAll(s, old, new *ast.MiniString) ast.MiniString {
	res := strings.ReplaceAll(s.GoString(), old.GoString(), new.GoString())
	return ast.NewMiniString(res)
}

func StrSplit(s, sep *ast.MiniString) []ast.MiniString {
	parts := strings.Split(s.GoString(), sep.GoString())
	res := make([]ast.MiniString, len(parts))
	for i, p := range parts {
		res[i] = ast.NewMiniString(p)
	}
	return res
}

func StrJoin(elems []ast.MiniString, sep *ast.MiniString) ast.MiniString {
	sElems := make([]string, len(elems))
	for i, e := range elems {
		sElems[i] = e.GoString()
	}
	return ast.NewMiniString(strings.Join(sElems, sep.GoString()))
}

func StrToUpper(s *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(strings.ToUpper(s.GoString()))
}

func StrToLower(s *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(strings.ToLower(s.GoString()))
}

func StrTrim(s, cutset *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(strings.Trim(s.GoString(), cutset.GoString()))
}

func StrTrimSpace(s *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(strings.TrimSpace(s.GoString()))
}

func StrIndex(s, substr *ast.MiniString) ast.MiniNumber {
	return ast.NewMiniNumber(int64(strings.Index(s.GoString(), substr.GoString())))
}

func StrRepeat(s *ast.MiniString, count *ast.MiniNumber) ast.MiniString {
	return ast.NewMiniString(strings.Repeat(s.GoString(), int(count.GoValue().(int64))))
}

func StrLength(s *ast.MiniString) ast.MiniNumber {
	return ast.NewMiniNumber(int64(len(s.GoString())))
}

func StrCount(s, substr *ast.MiniString) ast.MiniNumber {
	return ast.NewMiniNumber(int64(strings.Count(s.GoString(), substr.GoString())))
}

func StrEqualFold(s, t *ast.MiniString) ast.MiniBool {
	return ast.NewMiniBool(strings.EqualFold(s.GoString(), t.GoString()))
}

func StrFields(s *ast.MiniString) []ast.MiniString {
	parts := strings.Fields(s.GoString())
	res := make([]ast.MiniString, len(parts))
	for i, p := range parts {
		res[i] = ast.NewMiniString(p)
	}
	return res
}

func StrLastIndex(s, substr *ast.MiniString) ast.MiniNumber {
	return ast.NewMiniNumber(int64(strings.LastIndex(s.GoString(), substr.GoString())))
}

func StrTrimPrefix(s, prefix *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(strings.TrimPrefix(s.GoString(), prefix.GoString()))
}

func StrTrimSuffix(s, suffix *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(strings.TrimSuffix(s.GoString(), suffix.GoString()))
}
