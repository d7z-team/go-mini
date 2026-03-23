package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <file1.go> [file2.go ...]\n", os.Args[0])
		os.Exit(1)
	}

	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	converter := ffigo.NewGoToASTConverter()
	var rootProgram *ast.ProgramStmt

	// 1. 解析所有文件并合并到 rootProgram
	for i, arg := range os.Args[1:] {
		path, _ := filepath.Abs(arg)
		content, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("Error reading file %s: %v\n", arg, err)
			os.Exit(1)
		}

		node, err := converter.ConvertSource(string(content))
		if err != nil {
			fmt.Printf("Error converting file %s: %v\n", arg, err)
			os.Exit(1)
		}

		prog := node.(*ast.ProgramStmt)
		if i == 0 {
			rootProgram = prog
		} else {
			// 合并符号到第一个程序
			mergePrograms(rootProgram, prog)
		}
	}

	// 2. 创建运行时并运行
	runtime, err := executor.NewRuntimeByAst(rootProgram)
	if err != nil {
		fmt.Printf("Runtime initialization error: %v\n", err)
		os.Exit(1)
	}

	// 执行程序（内部会自动初始化全局变量并寻找 main() 函数运行）
	err = runtime.Execute(context.Background())
	if err != nil {
		fmt.Printf("Execution error: %v\n", err)
		os.Exit(1)
	}
}

func mergePrograms(dest, src *ast.ProgramStmt) {
	for k, v := range src.Functions {
		dest.Functions[k] = v
	}
	for k, v := range src.Structs {
		dest.Structs[k] = v
	}
	for k, v := range src.Variables {
		dest.Variables[k] = v
	}
	for k, v := range src.Constants {
		dest.Constants[k] = v
	}
	for k, v := range src.Interfaces {
		dest.Interfaces[k] = v
	}
	// 合并 main 代码块 (如果有的话)
	dest.Main = append(dest.Main, src.Main...)
}
