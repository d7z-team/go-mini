package runtimecheck

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	gotypes "go/types"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
)

// BufferLayout describes the Go frame scratch-buffer capacities observed by
// the guest census. Its logical slot cost remains bytecode.RuntimeSlotBytes.
type BufferLayout struct {
	ValueBytes      uint64   `json:"value_bytes"`
	PageBytes       uint64   `json:"page_bytes"`
	GrowthThreshold uint64   `json:"growth_threshold"`
	SmallCapacities []uint64 `json:"small_capacities"`
	DeferBytes      uint64   `json:"defer_bytes"`
	DeferCapacities []uint64 `json:"defer_capacities"`
}

func describeBufferLayout(sources fs.FS) (BufferLayout, error) {
	declarations := make(map[string]ast.Expr)
	for _, path := range []string{"runtime/value.go", "runtime/runtime_type.go", "compiler/types/model.go", "runtime/module.go", "runtime/scheduler_state.go"} {
		data, err := fs.ReadFile(sources, path)
		if err != nil {
			return BufferLayout{}, err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
		if err != nil {
			return BufferLayout{}, err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if declaration, ok := node.(*ast.TypeSpec); ok {
				declarations[declaration.Name.Name] = declaration.Type
				return false
			}
			return true
		})
	}
	var resolve func(ast.Expr) (gotypes.Type, error)
	resolve = func(expression ast.Expr) (gotypes.Type, error) {
		switch expression := expression.(type) {
		case *ast.Ident:
			if builtin := gotypes.Universe.Lookup(expression.Name); builtin != nil {
				return builtin.Type(), nil
			}
			declaration, found := declarations[expression.Name]
			if !found {
				return nil, fmt.Errorf("unknown storage type %s", expression.Name)
			}
			return resolve(declaration)
		case *ast.SelectorExpr:
			return resolve(expression.Sel)
		case *ast.StarExpr:
			return gotypes.NewPointer(gotypes.Typ[gotypes.Byte]), nil
		case *ast.MapType:
			return gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.NewPointer(gotypes.Typ[gotypes.Byte])), nil
		case *ast.InterfaceType:
			return gotypes.NewInterfaceType(nil, nil).Complete(), nil
		case *ast.StructType:
			var fields []*gotypes.Var
			for _, field := range expression.Fields.List {
				typ, err := resolve(field.Type)
				if err != nil {
					return nil, err
				}
				if len(field.Names) == 0 {
					return nil, errors.New("embedded field in guest storage layout")
				}
				for _, name := range field.Names {
					fields = append(fields, gotypes.NewField(token.NoPos, nil, name.Name, typ, false))
				}
			}
			return gotypes.NewStruct(fields, nil), nil
		default:
			return nil, fmt.Errorf("unsupported guest storage layout %T", expression)
		}
	}
	typ, err := resolve(&ast.Ident{Name: "vmValue"})
	if err != nil {
		return BufferLayout{}, err
	}
	sizes := gotypes.SizesFor("gc", runtime.GOARCH)
	if sizes == nil {
		return BufferLayout{}, fmt.Errorf("unsupported Go architecture %s", runtime.GOARCH)
	}
	valueBytes := sizes.Sizeof(typ)
	deferred, err := resolve(&ast.Ident{Name: "deferredCall"})
	if err != nil {
		return BufferLayout{}, err
	}
	deferBytes := sizes.Sizeof(deferred)
	pointerType := reflect.TypeFor[*byte]()
	goRoot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return BufferLayout{}, fmt.Errorf("locate Go runtime sources: %w", err)
	}
	readConstant := func(path, name string) (uint64, error) {
		data, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goRoot)), "src", path))
		if err != nil {
			return 0, err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
		if err != nil {
			return 0, err
		}
		var value uint64
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for index, identifier := range spec.Names {
				if identifier.Name == name && index < len(spec.Values) {
					if literal, ok := spec.Values[index].(*ast.BasicLit); ok {
						value, err = strconv.ParseUint(literal.Value, 0, 64)
					}
				}
			}
			return false
		})
		if err != nil {
			return 0, err
		}
		if value == 0 {
			return 0, fmt.Errorf("Go runtime constant %s unavailable in %s", name, path)
		}
		return value, nil
	}
	maxSmall, err := readConstant("internal/runtime/gc/sizeclasses.go", "MaxSmallSize")
	if err != nil {
		return BufferLayout{}, err
	}
	pageShift, err := readConstant("internal/runtime/gc/sizeclasses.go", "PageShift")
	if err != nil {
		return BufferLayout{}, err
	}
	threshold, err := readConstant("runtime/slice.go", "threshold")
	if err != nil {
		return BufferLayout{}, err
	}
	layout := BufferLayout{ValueBytes: uint64(valueBytes), DeferBytes: uint64(deferBytes), PageBytes: 1 << pageShift, GrowthThreshold: threshold}
	// A pointer-bearing array has the same size/alignment/allocator class as
	// vmValue. Append exposes actual capacities, including malloc headers.
	for _, buffer := range []struct {
		bytes      int64
		capacities *[]uint64
	}{{valueBytes, &layout.SmallCapacities}, {deferBytes, &layout.DeferCapacities}} {
		if buffer.bytes <= 0 || buffer.bytes%int64(pointerType.Size()) != 0 {
			return BufferLayout{}, fmt.Errorf("invalid guest buffer size %d", buffer.bytes)
		}
		sliceType := reflect.SliceOf(reflect.ArrayOf(int(buffer.bytes)/int(pointerType.Size()), pointerType))
		for length := 0; length <= int(maxSmall)/int(buffer.bytes)+1; length++ {
			values := reflect.MakeSlice(sliceType, length, length)
			grown := reflect.AppendSlice(reflect.MakeSlice(sliceType, 0, 0), values)
			*buffer.capacities = append(*buffer.capacities, uint64(grown.Cap()))
		}
	}
	return layout, nil
}
