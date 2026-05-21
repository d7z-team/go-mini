package runtime

import (
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
		Constants:        cloneStringMap(program.Constants),
		NamedTypes:       make(map[string]RuntimeType, len(program.Types)),
		StructSchemas:    make(map[string]*RuntimeStructSpec, len(program.Structs)),
		InterfaceSchemas: make(map[string]*RuntimeInterfaceSpec, len(program.Interfaces)),
		Globals:          make(map[string]*PreparedGlobal, len(program.Variables)),
		Functions:        make(map[string]*PreparedFunction, len(program.Functions)),
		MainTasks:        []Task{},
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
			FunctionSig: MustParseRuntimeFuncSig(fn.FunctionType.MiniType()),
			BodyTasks:   []Task{},
		}
	}
	return prepared, ValidatePreparedProgram(prepared)
}
