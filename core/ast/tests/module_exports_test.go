package ast_test

import "gopkg.d7z.net/go-mini/core/ast"

func registerModuleExports(validator *ast.ValidContext, path string, module *ast.ValidContext) {
	validator.Root().RegisterModuleExports(ast.NewModuleExportsFromRoot(path, module.Root()))
}
