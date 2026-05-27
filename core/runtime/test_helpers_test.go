package runtime

import (
	"fmt"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

func newExecutor(t *testing.T, program *ast.ProgramStmt) *Executor {
	t.Helper()

	prepared, err := preparedFromTestProgram(program)
	if err != nil {
		t.Fatalf("prepare program failed: %v", err)
	}
	exec, err := NewExecutorFromPrepared(prepared)
	if err != nil {
		t.Fatalf("new executor from prepared failed: %v", err)
	}
	return exec
}

func preparedFromTestProgram(program *ast.ProgramStmt) (*PreparedProgram, error) {
	if program == nil {
		program = &ast.ProgramStmt{}
	}
	prepared := &PreparedProgram{
		Package:          program.Package,
		Constants:        make(map[string]FFIConstValue, len(program.Constants)),
		ConstantTypes:    make(map[string]RuntimeType, len(program.ConstantTypes)),
		NamedTypes:       make(map[string]RuntimeType, len(program.Types)),
		StructSchemas:    make(map[string]*RuntimeStructSpec, len(program.Structs)),
		InterfaceSchemas: make(map[string]*RuntimeInterfaceSpec, len(program.Interfaces)),
		Globals:          make(map[string]*PreparedGlobal, len(program.Variables)),
		Functions:        make(map[string]*PreparedFunction, len(program.Functions)),
		MainTasks:        []Task{},
	}
	for name, typ := range program.ConstantTypes {
		parsed, err := ParseRuntimeType(typ)
		if err != nil {
			return nil, err
		}
		prepared.ConstantTypes[name] = parsed
	}
	for name, val := range program.Constants {
		typ, ok := prepared.ConstantTypes[name]
		if !ok {
			return nil, fmt.Errorf("constant %s missing type", name)
		}
		parsed, err := parseTestConstLiteral(val, typ)
		if err != nil {
			return nil, err
		}
		prepared.Constants[name] = parsed
	}
	for ident, typ := range program.Types {
		parsed, err := ParseRuntimeType(typ)
		if err != nil {
			return nil, err
		}
		prepared.NamedTypes[string(ident)] = parsed
	}
	for ident, stmt := range program.Structs {
		if stmt == nil {
			continue
		}
		fields := make([]ast.StructMemberType, 0, len(stmt.FieldNames))
		for _, fieldName := range stmt.FieldNames {
			fields = append(fields, ast.StructMemberType{Name: string(fieldName), Type: stmt.Fields[fieldName]})
		}
		spec, err := ParseRuntimeStructSpec(string(ident), StructOwnershipVMValue, ast.CreateStructType(fields))
		if err != nil {
			return nil, err
		}
		prepared.StructSchemas[string(ident)] = spec
	}
	for ident, stmt := range program.Interfaces {
		if stmt == nil {
			continue
		}
		spec, err := ParseRuntimeInterfaceSpec(stmt.Type)
		if err != nil {
			return nil, err
		}
		prepared.InterfaceSchemas[string(ident)] = spec
	}
	for ident, expr := range program.Variables {
		kind := MustParseRuntimeType(SpecAny)
		if expr != nil && !expr.GetBase().Type.IsEmpty() {
			kind = MustParseRuntimeType(expr.GetBase().Type)
		}
		name := string(ident)
		prepared.Globals[name] = &PreparedGlobal{Name: name, Kind: kind}
	}
	for ident, fn := range program.Functions {
		if fn == nil {
			continue
		}
		prepared.Functions[string(ident)] = &PreparedFunction{
			Name:        string(ident),
			Receiver:    TypeSpec(fn.ReceiverType),
			FunctionSig: MustParseRuntimeFuncSig(fn.FunctionType.MiniType()),
			BodyTasks:   []Task{},
		}
	}
	return prepared, ValidatePreparedProgram(prepared)
}

func parseTestConstLiteral(val string, typ RuntimeType) (FFIConstValue, error) {
	switch {
	case typ.IsString():
		return ConstString(val), nil
	case typ.IsInt():
		var parsed int64
		if _, err := fmt.Sscan(val, &parsed); err != nil {
			return FFIConstValue{}, err
		}
		return ConstInt64(parsed), nil
	case typ.Raw == SpecFloat64:
		var parsed float64
		if _, err := fmt.Sscan(val, &parsed); err != nil {
			return FFIConstValue{}, err
		}
		return ConstFloat64(parsed), nil
	case typ.IsBool():
		if val == "true" {
			return ConstBool(true), nil
		}
		if val == "false" {
			return ConstBool(false), nil
		}
		return FFIConstValue{}, fmt.Errorf("invalid bool literal %q", val)
	default:
		return FFIConstValue{}, fmt.Errorf("unsupported constant type %s", typ.Raw)
	}
}
