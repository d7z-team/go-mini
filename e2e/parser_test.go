package e2e_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

//go:embed testdata/for.json
var tf []byte

func TestFor(t *testing.T) {
	UtilsTest(t, tf, func(t *testing.T, v *ast.ValidContext) {
		assert.NoError(t, v.AddStructDefine("Browser", map[ast.Ident]ast.OPSType{
			"input": "function(Browser,String,String) String",
		}))
		assert.NoError(t, v.AddFuncSpec("openBrowser", "function(String)Browser"))
	})
}

//go:embed testdata/switch.json
var ts []byte

func TestSwitch(t *testing.T) {
	UtilsTest(t, ts, func(t *testing.T, v *ast.ValidContext) {
		assert.NoError(t, v.AddFuncSpec("print", "function(Int64)"))
	})
}

//go:embed testdata/struct.json
var stru []byte

func TestStruct(t *testing.T) {
	UtilsTest(t, stru, func(t *testing.T, v *ast.ValidContext) {
		assert.NoError(t, v.AddFuncSpec("print", "function(Int64)"))
	})
}

//go:embed testdata/test.json
var test []byte

func TestTest(t *testing.T) {
	UtilsTest(t, test, func(t *testing.T, v *ast.ValidContext) {
		assert.NoError(t, v.AddFuncSpec("print", "function(Int64)"))
	})
}

func UtilsTest(t *testing.T, astData []byte, call func(t *testing.T, v *ast.ValidContext)) {
	data, err := engine.Unmarshal(astData)
	if err != nil {
		t.Fatal(err)
	}
	optimize, logs, err := engine.ValidateAndOptimize(data, func(v *ast.ValidContext) error {
		call(t, v)
		return nil
	})

	if len(logs) > 0 || err != nil {
		var errorMsgs []string
		for _, err := range logs {
			errorMsgs = append(errorMsgs, fmt.Sprintf("节点 [%s] %s", strings.Join(err.Path, "->"), err.Message))
		}
		t.Logf("AST验证错误:\n  %s\n", strings.Join(errorMsgs, "\n  "))
	} else {
		var decode bytes.Buffer
		encoder := json.NewEncoder(&decode)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(optimize)
		t.Log(decode.String())
	}
}
