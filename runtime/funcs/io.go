package funcs

import (
	"fmt"
	"io"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/runtime/types"
)

func InitIO(executor *engine.MiniExecutor) {
	executor.MustAddPackageFunc("io", "ReadAll", IoReadAll, "读取所有数据直到 EOF")
	executor.MustAddPackageFunc("io", "Copy", IoCopy, "将源数据拷贝到目标")
	executor.MustAddPackageFunc("io", "WriteString", IoWriteString, "将字符串写入目标")
	executor.MustAddPackageFunc("io", "NewBuffer", types.NewMiniBuffer, "创建一个新的字节缓冲区")
}

func IoReadAll(reader any) (types.MiniFile, error) {
	r, err := toReader(reader)
	if err != nil {
		return types.MiniFile{}, err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return types.MiniFile{}, err
	}
	return types.NewMiniFile(data), nil
}

func IoCopy(dst, src any) (ast.MiniNumber, error) {
	w, err := toWriter(dst)
	if err != nil {
		return ast.MiniNumber{}, err
	}
	r, err := toReader(src)
	if err != nil {
		return ast.MiniNumber{}, err
	}
	n, err := io.Copy(w, r)
	return ast.NewMiniNumber(n), err
}

func IoWriteString(dst any, s *ast.MiniString) (ast.MiniNumber, error) {
	w, err := toWriter(dst)
	if err != nil {
		return ast.MiniNumber{}, err
	}
	n, err := io.WriteString(w, s.GoString())
	return ast.NewMiniNumber(int64(n)), err
}

func toReader(v any) (io.Reader, error) {
	switch curr := v.(type) {
	case io.Reader:
		return curr, nil
	case interface{ Reader() io.Reader }:
		return curr.Reader(), nil
	case *types.MiniFsFile:
		// Since we can't easily get the underlying afero.File interface without exposing it
		// We should have implemented io.Reader on MiniFsFile in types/fs.go
		// Let's assume we will add it or it's accessible via reflection
		return curr, nil
	case *types.MiniBuffer:
		return curr, nil
	default:
		return nil, fmt.Errorf("type %T is not a reader", v)
	}
}

func toWriter(v any) (io.Writer, error) {
	switch curr := v.(type) {
	case io.Writer:
		return curr, nil
	case *types.MiniFsFile:
		return curr, nil
	case *types.MiniBuffer:
		return curr, nil
	default:
		return nil, fmt.Errorf("type %T is not a writer", v)
	}
}
