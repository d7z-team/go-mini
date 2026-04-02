package bytecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

const (
	FormatGoMiniBytecode = "go-mini-bytecode"
	CurrentVersion       = 1
)

type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Instruction struct {
	Op      string    `json:"op"`
	Operand string    `json:"operand"`
	NodeID  string    `json:"node_id,omitempty"`
	Loc     *Location `json:"loc,omitempty"`
	Comment string    `json:"comment,omitempty"`
}

type Global struct {
	Name         string        `json:"name"`
	Instructions []Instruction `json:"instructions"`
}

type Function struct {
	Name         string        `json:"name"`
	Signature    string        `json:"signature"`
	Instructions []Instruction `json:"instructions"`
}

type Program struct {
	Format     string                   `json:"format"`
	Version    int                      `json:"version"`
	OpcodeSet  string                   `json:"opcode_set"`
	Globals    []Global                 `json:"globals"`
	Entry      []Instruction            `json:"entry"`
	Functions  []Function               `json:"functions"`
	Blueprint  *Blueprint               `json:"blueprint,omitempty"`
	Executable *runtime.PreparedProgram `json:"executable,omitempty"`
}

type Blueprint struct {
	Package    string                           `json:"package,omitempty"`
	Constants  map[string]string                `json:"constants,omitempty"`
	Types      map[ast.Ident]ast.GoMiniType     `json:"types,omitempty"`
	Structs    map[ast.Ident]*ast.StructStmt    `json:"structs,omitempty"`
	Interfaces map[ast.Ident]*ast.InterfaceStmt `json:"interfaces,omitempty"`
}

func NewProgram() *Program {
	return &Program{
		Format:    FormatGoMiniBytecode,
		Version:   CurrentVersion,
		OpcodeSet: "runtime.opcode.v1",
		Globals:   make([]Global, 0),
		Entry:     make([]Instruction, 0),
		Functions: make([]Function, 0),
	}
}

func (p *Program) Validate() error {
	if p == nil {
		return errors.New("nil bytecode program")
	}
	if p.Format == "" {
		return errors.New("missing bytecode format")
	}
	if p.Format != FormatGoMiniBytecode {
		return fmt.Errorf("unsupported bytecode format: %s", p.Format)
	}
	if p.Version <= 0 {
		return fmt.Errorf("invalid bytecode version: %d", p.Version)
	}
	if p.Version != CurrentVersion {
		return fmt.Errorf("unsupported bytecode version: %d", p.Version)
	}
	if p.OpcodeSet == "" {
		return errors.New("missing opcode set")
	}
	if p.Executable != nil && p.Blueprint == nil {
		return errors.New("missing blueprint for executable bytecode")
	}
	for _, global := range p.Globals {
		if global.Name == "" {
			return errors.New("bytecode global missing name")
		}
		if err := validateInstructions(global.Instructions); err != nil {
			return fmt.Errorf("invalid global %s: %w", global.Name, err)
		}
	}
	if err := validateInstructions(p.Entry); err != nil {
		return fmt.Errorf("invalid entry: %w", err)
	}
	for _, fn := range p.Functions {
		if fn.Name == "" {
			return errors.New("bytecode function missing name")
		}
		if err := validateInstructions(fn.Instructions); err != nil {
			return fmt.Errorf("invalid function %s: %w", fn.Name, err)
		}
	}
	return nil
}

func NewBlueprint(program *ast.ProgramStmt) *Blueprint {
	if program == nil {
		return nil
	}
	blueprint := &Blueprint{
		Package:    program.Package,
		Constants:  cloneStringMap(program.Constants),
		Types:      cloneTypesMap(program.Types),
		Structs:    cloneStructs(program.Structs),
		Interfaces: cloneInterfaces(program.Interfaces),
	}
	return blueprint
}

func (p *Program) RebuildProgram() (*ast.ProgramStmt, error) {
	if p == nil {
		return nil, errors.New("nil bytecode program")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	prog := &ast.ProgramStmt{
		BaseNode:   ast.BaseNode{ID: "bytecode", Meta: "boot", Type: "Void"},
		Package:    "main",
		Constants:  make(map[string]string),
		Variables:  make(map[ast.Ident]ast.Expr),
		Types:      make(map[ast.Ident]ast.GoMiniType),
		Structs:    make(map[ast.Ident]*ast.StructStmt),
		Interfaces: make(map[ast.Ident]*ast.InterfaceStmt),
		Functions:  make(map[ast.Ident]*ast.FunctionStmt),
		Main:       []ast.Stmt{},
	}
	if p.Blueprint != nil {
		if p.Blueprint.Package != "" {
			prog.Package = p.Blueprint.Package
		}
		prog.Constants = cloneStringMap(p.Blueprint.Constants)
		prog.Types = cloneTypesMap(p.Blueprint.Types)
		prog.Structs = cloneStructs(p.Blueprint.Structs)
		prog.Interfaces = cloneInterfaces(p.Blueprint.Interfaces)
	}
	if p.Executable != nil {
		for name := range p.Executable.Globals {
			prog.Variables[name] = nil
		}
		for name, fn := range p.Executable.Functions {
			if fn == nil {
				continue
			}
			prog.Functions[name] = &ast.FunctionStmt{
				BaseNode:     ast.BaseNode{ID: "bytecode_fn_" + string(name)},
				FunctionType: fn.FunctionType,
				Name:         name,
				Body:         &ast.BlockStmt{Children: []ast.Stmt{}, Inner: true},
			}
		}
	}
	return prog, nil
}

func UnmarshalJSON(data []byte) (*Program, error) {
	var program Program
	if err := json.Unmarshal(data, &program); err != nil {
		return nil, err
	}
	if err := program.Validate(); err != nil {
		return nil, err
	}
	return &program, nil
}

func (p *Program) Disassemble() string {
	if p == nil {
		return "; Error: no bytecode loaded\n"
	}

	var sb strings.Builder
	sb.WriteString("; Go-Mini Bytecode Disassembly\n")
	fmt.Fprintf(&sb, "; Format: %s v%d (%s)\n", p.Format, p.Version, p.OpcodeSet)
	fmt.Fprintf(&sb, "; Total Globals: %d\n", len(p.Globals))
	fmt.Fprintf(&sb, "; Total Functions: %d\n\n", len(p.Functions))

	sb.WriteString("section .data:\n")
	for _, global := range p.Globals {
		fmt.Fprintf(&sb, "  global %s\n", global.Name)
		writeInstructions(&sb, "    ", global.Instructions)
	}
	sb.WriteString("\n")

	sb.WriteString("section .text:\n")
	sb.WriteString("main:\n")
	writeInstructions(&sb, "  ", p.Entry)
	if len(p.Entry) > 0 {
		sb.WriteString("\n")
	}

	for _, fn := range p.Functions {
		fmt.Fprintf(&sb, "%s%s:\n", fn.Name, fn.Signature)
		writeInstructions(&sb, "  ", fn.Instructions)
		sb.WriteString("\n")
	}

	return sb.String()
}

func writeInstructions(sb *strings.Builder, indent string, instructions []Instruction) {
	for _, inst := range instructions {
		addr := "[                ]"
		if inst.NodeID != "" {
			addr = fmt.Sprintf("[%16s]", inst.NodeID)
		}

		operand := strings.ReplaceAll(inst.Operand, "\n", "\\n")
		operand = strings.ReplaceAll(operand, "\r", "\\r")

		comment := inst.Comment
		if inst.Loc != nil {
			if comment != "" {
				comment = fmt.Sprintf("%s at L%d:%d", comment, inst.Loc.Line, inst.Loc.Column)
			} else {
				comment = fmt.Sprintf("at L%d:%d", inst.Loc.Line, inst.Loc.Column)
			}
		}

		line := fmt.Sprintf("%s  %-18s %-22s", addr, inst.Op, operand)
		if comment != "" {
			line = fmt.Sprintf("%-65s ; %s", line, comment)
		}
		sb.WriteString(indent + line + "\n")
	}
}

func validateInstructions(instructions []Instruction) error {
	for idx, inst := range instructions {
		if inst.Op == "" {
			return fmt.Errorf("instruction[%d] missing opcode", idx)
		}
	}
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return make(map[string]string)
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneTypesMap(in map[ast.Ident]ast.GoMiniType) map[ast.Ident]ast.GoMiniType {
	if len(in) == 0 {
		return make(map[ast.Ident]ast.GoMiniType)
	}
	out := make(map[ast.Ident]ast.GoMiniType, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStructs(in map[ast.Ident]*ast.StructStmt) map[ast.Ident]*ast.StructStmt {
	if len(in) == 0 {
		return make(map[ast.Ident]*ast.StructStmt)
	}
	out := make(map[ast.Ident]*ast.StructStmt, len(in))
	for k, v := range in {
		if v == nil {
			out[k] = nil
			continue
		}
		fields := make(map[ast.Ident]ast.GoMiniType, len(v.Fields))
		for fieldName, fieldType := range v.Fields {
			fields[fieldName] = fieldType
		}
		out[k] = &ast.StructStmt{
			BaseNode:   v.BaseNode,
			Name:       v.Name,
			Fields:     fields,
			FieldNames: append([]ast.Ident(nil), v.FieldNames...),
			Doc:        v.Doc,
		}
	}
	return out
}

func cloneInterfaces(in map[ast.Ident]*ast.InterfaceStmt) map[ast.Ident]*ast.InterfaceStmt {
	if len(in) == 0 {
		return make(map[ast.Ident]*ast.InterfaceStmt)
	}
	out := make(map[ast.Ident]*ast.InterfaceStmt, len(in))
	for k, v := range in {
		if v == nil {
			out[k] = nil
			continue
		}
		out[k] = &ast.InterfaceStmt{
			BaseNode: v.BaseNode,
			Name:     v.Name,
			Type:     v.Type,
		}
	}
	return out
}
