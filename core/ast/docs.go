package ast

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"runtime"
	"strings"
	"sync"
)

var (
	docsCache = make(map[string]map[string]string)
	docsMutex sync.Mutex
)

// GetFuncDoc returns the documentation for a given function or method.
// It parses the Go source file at runtime to extract the comments.
func GetFuncDoc(f any) string {
	val := reflect.ValueOf(f)
	if val.Kind() != reflect.Func {
		return ""
	}

	pc := val.Pointer()
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return ""
	}

	file, _ := fn.FileLine(pc)
	if file == "" {
		return ""
	}

	// or "gopkg.d7z.net/go-mini/core/ast.(*MiniString).Clone"
	name := fn.Name()

	// Extract the short name (e.g., "Sleep" or "MiniString.Clone")
	parts := strings.Split(name, "/")
	lastPart := parts[len(parts)-1]

	dotParts := strings.Split(lastPart, ".")
	var searchKey string

	if len(dotParts) > 1 {
		// Method: pkg.(*Type).Method -> Type.Method
		if strings.HasPrefix(dotParts[1], "(*") {
			typeName := strings.TrimSuffix(strings.TrimPrefix(dotParts[1], "(*"), ")")
			searchKey = typeName + "." + dotParts[2]
		} else if len(dotParts) == 2 {
			// Function: pkg.Func -> Func
			searchKey = dotParts[1]
		} else if len(dotParts) == 3 {
			// value method: pkg.Type.Method -> Type.Method
			searchKey = dotParts[1] + "." + dotParts[2]
		}
	} else {
		searchKey = lastPart
	}

	if searchKey == "" {
		return ""
	}

	return getDocFromFile(file, searchKey)
}

func getDocFromFile(file, key string) string {
	docsMutex.Lock()
	defer docsMutex.Unlock()

	if docs, ok := docsCache[file]; ok {
		return docs[key]
	}

	docs := make(map[string]string)
	docsCache[file] = docs

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		return ""
	}

	for _, decl := range astFile.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok && fnDecl.Doc != nil {
			var receiverName string
			if fnDecl.Recv != nil && len(fnDecl.Recv.List) > 0 {
				switch t := fnDecl.Recv.List[0].Type.(type) {
				case *ast.StarExpr:
					if ident, ok := t.X.(*ast.Ident); ok {
						receiverName = ident.Name
					}
				case *ast.Ident:
					receiverName = t.Name
				}
			}

			declKey := fnDecl.Name.Name
			if receiverName != "" {
				declKey = receiverName + "." + declKey
			}

			doc := strings.TrimSpace(fnDecl.Doc.Text())
			if doc != "" {
				docs[declKey] = doc
			}
		}
	}

	return docs[key]
}
