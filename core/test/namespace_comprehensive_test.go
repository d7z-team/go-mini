package test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

// 模拟一个复杂的原生包：数据库 db
type DBConfig struct {
	URL ast.MiniString
}

func (c *DBConfig) OPSType() ast.Ident { return "db.Config" }

type Connection struct {
	ID ast.MiniString
}

func (c *Connection) OPSType() ast.Ident { return "db.Connection" }
func (c *Connection) Query(sql *ast.MiniString) *ast.MiniString {
	s := ast.NewMiniString(fmt.Sprintf("result of [%s] on %s", sql.GoString(), c.ID.GoString()))
	return &s
}

func TestComprehensiveNamespace(t *testing.T) {
	e := engine.NewMiniExecutor()

	// 1. 注入 db 包
	e.AddPackageStruct("db", "Config", (*DBConfig)(nil))
	e.AddPackageStruct("db", "Connection", (*Connection)(nil))
	e.MustAddPackageFunc("db", "Connect", func(cfg *DBConfig) *Connection {
		s := ast.NewMiniString("conn-" + cfg.URL.GoString())
		return &Connection{ID: s}
	})

	// 2. 注入 math 包
	e.MustAddPackageFunc("math", "Add", func(a, b *ast.MiniInt64) *ast.MiniInt64 {
		res := ast.NewMiniInt64(a.GoValue().(int64) + b.GoValue().(int64))
		return &res
	})

	// 3. 注入系统工具包，包含一个“私有”函数（小写开头）
	e.MustAddPackageFunc("utils", "PublicFunc", func() *ast.MiniString {
		s := ast.NewMiniString("public")
		return &s
	})
	// 尽管 Go 层可以注册，但引擎验证层应该拦截对它的跨包调用
	e.MustAddPackageFunc("utils", "privateFunc", func() *ast.MiniString {
		s := ast.NewMiniString("private")
		return &s
	})

	var results []string
	e.MustAddFunc("push", func(s *ast.MiniString) {
		results = append(results, s.GoString())
	})

	t.Run("BasicNamespaceAndAlias", func(t *testing.T) {
		results = nil
		code := `
package main
import "math"
import d "db"

func main() {
	// 测试别名和跨包调用
	cfg := d.Config{URL: "localhost:5432"}
	conn := d.Connect(&cfg)
	res := conn.Query("SELECT *")
	push(res)

	// 测试标准导入
	sum := math.Add(10, 20)
	push(sum.String())
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = prog.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"result of [SELECT *] on conn-localhost:5432", "30"}, results)
	})

	t.Run("VisibilityViolation", func(t *testing.T) {
		code := `
package main
import "utils"
func main() {
	utils.privateFunc()
}
`
		_, err := e.NewRuntimeByGoCode(code)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot refer to unexported name utils.privateFunc")
	})

	t.Run("GlobalVarNamespace", func(t *testing.T) {
		results = nil
		libCode := `
package config
var ApiKey = "secret-123"
func GetKey() string {
	return ApiKey
}
`
		prog, err := e.NewRuntimeByGoCode(libCode)
		assert.NoError(t, err)

		libAst := prog.GetProgram()
		assert.NotNil(t, libAst)

		// 验证能够识别 config.ApiKey 这个符号
		// 在这里我们检查 libAst.Functions["config.GetKey"] 的 body 是否正确指向了 config.ApiKey
		foundMangledRef := false
		for name, f := range libAst.Functions {
			if name == "config.GetKey" {
				ret := f.Body.Children[0].(*ast.ReturnStmt)
				ident := ret.Results[0].(*ast.IdentifierExpr)
				if ident.Name == "config.ApiKey" {
					foundMangledRef = true
				}
			}
		}
		assert.True(t, foundMangledRef, "Reference to ApiKey inside config package should be mangled to config.ApiKey")
	})

	t.Run("SamePackageInternalAccess", func(t *testing.T) {
		// 验证同一个包内访问全局变量不需要前缀
		code := `
package mypkg
var Counter = 0
func Inc() {
	Counter = 100 
}
func Get() Int64 {
	return Counter
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)

		libAst := prog.GetProgram()

		// 查找 Get 函数的返回语句
		foundMangledRef := false
		for name, f := range libAst.Functions {
			if name == "mypkg.Get" {
				ret := f.Body.Children[0].(*ast.ReturnStmt)
				ident := ret.Results[0].(*ast.IdentifierExpr)
				if ident.Name == "mypkg.Counter" {
					foundMangledRef = true
				}
			}
		}
		assert.True(t, foundMangledRef, "Internal reference to Counter should be mangled to mypkg.Counter")
	})
}
