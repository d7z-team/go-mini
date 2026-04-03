package bytecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	sb.WriteString("; go-mini bytecode disassembly\n")
	fmt.Fprintf(&sb, "; format     %s v%d\n", p.Format, p.Version)
	fmt.Fprintf(&sb, "; opcode_set %s\n", p.OpcodeSet)
	if p.Blueprint != nil && p.Blueprint.Package != "" {
		fmt.Fprintf(&sb, "; package    %s\n", p.Blueprint.Package)
	}
	fmt.Fprintf(&sb, "; globals    display=%d executable=%d\n", len(p.Globals), len(disassembleExecutableGlobals(p)))
	fmt.Fprintf(&sb, "; functions  display=%d executable=%d\n", len(p.Functions), len(disassembleExecutableFunctions(p)))
	sb.WriteString("\n")

	writeBlueprintSection(&sb, p)
	writeGlobalsSections(&sb, p)
	writeTextSection(&sb, p)

	return sb.String()
}

func writeBlueprintSection(sb *strings.Builder, p *Program) {
	if p == nil || p.Blueprint == nil {
		return
	}

	if len(p.Blueprint.Constants) > 0 {
		sb.WriteString("section .rodata\n")
		keys := make([]string, 0, len(p.Blueprint.Constants))
		for name := range p.Blueprint.Constants {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			fmt.Fprintf(sb, "const.%s: db %q, 0\n", sanitizeLabel(name), p.Blueprint.Constants[name])
		}
		sb.WriteString("\n")
	}

	if len(p.Blueprint.Types) > 0 || len(p.Blueprint.Structs) > 0 || len(p.Blueprint.Interfaces) > 0 {
		sb.WriteString("section .note.go_mini\n")
		writeBlueprintNotes(sb, p)
		sb.WriteString("\n")
	}
}

func writeGlobalsSections(sb *strings.Builder, p *Program) {
	initGlobals := append([]Global(nil), p.Globals...)
	sort.Slice(initGlobals, func(i, j int) bool {
		return initGlobals[i].Name < initGlobals[j].Name
	})

	execGlobals := disassembleExecutableGlobals(p)
	uninitialized := make([]string, 0)
	for name, global := range execGlobals {
		if global != nil && !global.HasInit {
			uninitialized = append(uninitialized, name)
		}
	}
	sort.Strings(uninitialized)

	if len(uninitialized) > 0 {
		sb.WriteString("section .bss\n")
		for _, name := range uninitialized {
			fmt.Fprintf(sb, "global.%s: resq 1\n", sanitizeLabel(name))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("section .data\n")
	if len(initGlobals) == 0 {
		sb.WriteString("; no global initializers\n\n")
		return
	}
	for _, global := range initGlobals {
		meta := ""
		if exec := execGlobals[global.Name]; exec != nil {
			meta = fmt.Sprintf(" ; has_init=%t", exec.HasInit)
		}
		fmt.Fprintf(sb, "global.%s:%s\n", sanitizeLabel(global.Name), meta)
		writeInstructions(sb, global.Instructions)
		sb.WriteString("\n")
	}
}

func writeTextSection(sb *strings.Builder, p *Program) {
	sb.WriteString("section .text\n")
	sb.WriteString("global _start\n\n")
	sb.WriteString("_start:\n")
	if len(p.Entry) == 0 {
		sb.WriteString("    ; no entry instructions\n")
	} else {
		writeInstructions(sb, p.Entry)
	}
	sb.WriteString("\n")

	displayFunctions := make(map[string]Function, len(p.Functions))
	for _, fn := range p.Functions {
		displayFunctions[fn.Name] = fn
	}
	names := make([]string, 0, len(displayFunctions))
	for name := range displayFunctions {
		names = append(names, name)
	}
	for name := range disassembleExecutableFunctions(p) {
		if _, ok := displayFunctions[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	execFunctions := disassembleExecutableFunctions(p)
	for _, name := range names {
		fn, hasDisplay := displayFunctions[name]
		signature := ""
		if hasDisplay {
			signature = fn.Signature
		}
		if signature == "" {
			if exec := execFunctions[name]; exec != nil {
				signature = formatExecutableSignature(exec)
			}
		}
		if signature != "" {
			fmt.Fprintf(sb, "fn.%s: ; signature %s\n", sanitizeLabel(name), signature)
		} else {
			fmt.Fprintf(sb, "fn.%s:\n", sanitizeLabel(name))
		}
		if hasDisplay && len(fn.Instructions) > 0 {
			writeInstructions(sb, fn.Instructions)
		} else if exec := execFunctions[name]; exec != nil {
			fmt.Fprintf(sb, "    ; executable-only body (%d prepared tasks)\n", len(exec.BodyTasks))
		} else {
			sb.WriteString("    ; no body\n")
		}
		sb.WriteString("\n")
	}
}

func writeInstructions(sb *strings.Builder, instructions []Instruction) {
	for pc, inst := range instructions {
		operand := strings.ReplaceAll(inst.Operand, "\n", "\\n")
		operand = strings.ReplaceAll(operand, "\r", "\\r")
		line := fmt.Sprintf("    %04d  %-18s %s", pc, inst.Op, operand)
		comment := instructionComment(inst)
		if comment != "" {
			line = fmt.Sprintf("%-64s ; %s", line, comment)
		}
		sb.WriteString(line + "\n")
	}
}

func instructionComment(inst Instruction) string {
	parts := make([]string, 0, 3)
	if inst.Comment != "" {
		parts = append(parts, inst.Comment)
	}
	if inst.NodeID != "" {
		parts = append(parts, "node="+inst.NodeID)
	}
	if inst.Loc != nil {
		if inst.Loc.File != "" {
			parts = append(parts, fmt.Sprintf("%s:%d:%d", inst.Loc.File, inst.Loc.Line, inst.Loc.Column))
		} else {
			parts = append(parts, fmt.Sprintf("L%d:%d", inst.Loc.Line, inst.Loc.Column))
		}
	}
	return strings.Join(parts, " | ")
}

func writeBlueprintNotes(sb *strings.Builder, p *Program) {
	if p == nil || p.Blueprint == nil {
		return
	}
	typeKeys := make([]string, 0, len(p.Blueprint.Types))
	for name := range p.Blueprint.Types {
		typeKeys = append(typeKeys, string(name))
	}
	sort.Strings(typeKeys)
	for _, name := range typeKeys {
		fmt.Fprintf(sb, "; type %s = %s\n", name, p.Blueprint.Types[ast.Ident(name)])
	}

	structKeys := make([]string, 0, len(p.Blueprint.Structs))
	for name := range p.Blueprint.Structs {
		structKeys = append(structKeys, string(name))
	}
	sort.Strings(structKeys)
	for _, name := range structKeys {
		spec := p.Blueprint.Structs[ast.Ident(name)]
		if spec == nil {
			fmt.Fprintf(sb, "; struct %s\n", name)
			continue
		}
		fields := make([]string, 0, len(spec.FieldNames))
		for _, fieldName := range spec.FieldNames {
			fields = append(fields, fmt.Sprintf("%s %s", fieldName, spec.Fields[fieldName]))
		}
		fmt.Fprintf(sb, "; struct %s { %s }\n", name, strings.Join(fields, "; "))
	}

	ifaceKeys := make([]string, 0, len(p.Blueprint.Interfaces))
	for name := range p.Blueprint.Interfaces {
		ifaceKeys = append(ifaceKeys, string(name))
	}
	sort.Strings(ifaceKeys)
	for _, name := range ifaceKeys {
		spec := p.Blueprint.Interfaces[ast.Ident(name)]
		if spec == nil {
			fmt.Fprintf(sb, "; interface %s\n", name)
			continue
		}
		fmt.Fprintf(sb, "; interface %s %s\n", name, spec.Type)
	}
}

func disassembleExecutableGlobals(p *Program) map[string]*runtime.PreparedGlobal {
	if p == nil || p.Executable == nil {
		return nil
	}
	out := make(map[string]*runtime.PreparedGlobal, len(p.Executable.Globals))
	for name, global := range p.Executable.Globals {
		out[string(name)] = global
	}
	return out
}

func disassembleExecutableFunctions(p *Program) map[string]*runtime.PreparedFunction {
	if p == nil || p.Executable == nil {
		return nil
	}
	out := make(map[string]*runtime.PreparedFunction, len(p.Executable.Functions))
	for name, fn := range p.Executable.Functions {
		out[string(name)] = fn
	}
	return out
}

func formatExecutableSignature(fn *runtime.PreparedFunction) string {
	if fn == nil {
		return ""
	}
	return fn.FunctionType.String()
}

func sanitizeLabel(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		".", "_",
		"-", "_",
		" ", "_",
		":", "_",
	)
	return replacer.Replace(name)
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
