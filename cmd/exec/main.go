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

		node, err := converter.ConvertSource(arg, string(content))
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

	// 2. 语义校验和创建运行时
	runtime, errs := executor.NewMiniProgramByAstTolerant(rootProgram)
	if len(errs) > 0 {
		fmt.Println("Semantic validation failed:")
		for _, e := range errs {
			if astErr, ok := e.(*ast.MiniAstError); ok {
				for _, log := range astErr.Logs {
					loc := log.Node.GetBase().Loc
					fmt.Printf("[%s:%d:%d] %s\n", loc.F, loc.L, loc.C, log.Message)
				}
			} else {
				fmt.Println(e.Error())
			}
		}
		os.Exit(1)
	}

	if runtime == nil {
		fmt.Printf("Runtime initialization failed.\n")
		os.Exit(1)
	}

	// 执行程序（内部会自动初始化全局变量并寻找 main() 函数运行）
	err := runtime.Execute(context.Background())
	if err != nil {
		fmt.Printf("%s\n", err.Error())
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
