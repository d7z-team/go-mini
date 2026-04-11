package bytecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
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
				FunctionType: fn.FunctionSig.FunctionType(),
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
			writePreparedTasks(sb, exec.BodyTasks, "    ", "fn."+sanitizeLabel(name))
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

type preparedDisasmState struct {
	nextLabel int
}

func writePreparedTasks(sb *strings.Builder, tasks []runtime.Task, indent, scope string) {
	pc := 0
	state := &preparedDisasmState{}
	writePreparedTaskBlock(sb, tasks, indent, scope, &pc, state)
}

func writePreparedTaskBlock(sb *strings.Builder, tasks []runtime.Task, indent, scope string, pc *int, state *preparedDisasmState) {
	if len(tasks) == 0 {
		sb.WriteString(indent + "; no body\n")
		return
	}
	for _, task := range tasks {
		children := preparedTaskChildren(task, scope, state)
		operand := formatPreparedTaskOperand(task)
		line := fmt.Sprintf("%s%04d  %-18s %s", indent, *pc, task.Op.String(), operand)
		comment := preparedTaskComment(task, children)
		if comment != "" {
			line = fmt.Sprintf("%-64s ; %s", line, comment)
		}
		sb.WriteString(strings.TrimRight(line, " ") + "\n")
		*pc = *pc + 1
		for _, child := range children {
			sb.WriteString(indent + child.label + ":\n")
			writePreparedTaskBlock(sb, child.tasks, indent+"    ", child.label, pc, state)
		}
	}
}

type preparedTaskChildBlock struct {
	label string
	tasks []runtime.Task
}

func preparedTaskChildren(task runtime.Task, scope string, state *preparedDisasmState) []preparedTaskChildBlock {
	newLabel := func(suffix string) string {
		label := fmt.Sprintf("%s.L%04d%s", scope, state.nextLabel, suffix)
		state.nextLabel++
		return label
	}
	switch data := task.Data.(type) {
	case *runtime.BranchData:
		blocks := make([]preparedTaskChildBlock, 0, 2)
		if len(data.Then) > 0 {
			blocks = append(blocks, preparedTaskChildBlock{label: newLabel(".then"), tasks: data.Then})
		}
		if len(data.Else) > 0 {
			blocks = append(blocks, preparedTaskChildBlock{label: newLabel(".else"), tasks: data.Else})
		}
		return blocks
	case *runtime.ForData:
		blocks := make([]preparedTaskChildBlock, 0, 3)
		if len(data.Cond) > 0 {
			blocks = append(blocks, preparedTaskChildBlock{label: newLabel(".cond"), tasks: data.Cond})
		}
		if len(data.Body) > 0 {
			blocks = append(blocks, preparedTaskChildBlock{label: newLabel(".body"), tasks: data.Body})
		}
		if len(data.Update) > 0 {
			blocks = append(blocks, preparedTaskChildBlock{label: newLabel(".update"), tasks: data.Update})
		}
		return blocks
	case *runtime.DeferData:
		if len(data.Tasks) == 0 {
			return nil
		}
		return []preparedTaskChildBlock{{label: newLabel(".defer"), tasks: data.Tasks}}
	case *runtime.FinallyData:
		if len(data.Body) == 0 {
			return nil
		}
		return []preparedTaskChildBlock{{label: newLabel(".finally"), tasks: data.Body}}
	case *runtime.CatchData:
		if len(data.Body) == 0 {
			return nil
		}
		return []preparedTaskChildBlock{{label: newLabel(".catch"), tasks: data.Body}}
	case *runtime.RangeData:
		if len(data.Body) == 0 {
			return nil
		}
		return []preparedTaskChildBlock{{label: newLabel(".range_body"), tasks: data.Body}}
	case *runtime.SwitchData:
		blocks := make([]preparedTaskChildBlock, 0, 2+len(data.Cases))
		if len(data.Init) > 0 {
			blocks = append(blocks, preparedTaskChildBlock{label: newLabel(".init"), tasks: data.Init})
		}
		if len(data.Tag) > 0 {
			blocks = append(blocks, preparedTaskChildBlock{label: newLabel(".tag"), tasks: data.Tag})
		}
		if len(data.AssignLHS) > 0 {
			blocks = append(blocks, preparedTaskChildBlock{label: newLabel(".assign_lhs"), tasks: data.AssignLHS})
		}
		for idx, c := range data.Cases {
			for exprIdx, exprTasks := range c.Exprs {
				if len(exprTasks) > 0 {
					blocks = append(blocks, preparedTaskChildBlock{
						label: newLabel(fmt.Sprintf(".case_%d_match_%d", idx, exprIdx)),
						tasks: exprTasks,
					})
				}
			}
			if len(c.Body) > 0 {
				blocks = append(blocks, preparedTaskChildBlock{
					label: newLabel(fmt.Sprintf(".case_%d", idx)),
					tasks: c.Body,
				})
			}
		}
		if len(data.DefaultBody) > 0 {
			blocks = append(blocks, preparedTaskChildBlock{label: newLabel(".default"), tasks: data.DefaultBody})
		}
		return blocks
	case *runtime.JumpData:
		if len(data.Right) == 0 {
			return nil
		}
		return []preparedTaskChildBlock{{label: newLabel(".rhs"), tasks: data.Right}}
	case *runtime.ClosureData:
		if len(data.BodyTasks) == 0 {
			return nil
		}
		return []preparedTaskChildBlock{{label: newLabel(".closure_body"), tasks: data.BodyTasks}}
	case *runtime.DoCallData:
		if len(data.BodyTasks) == 0 {
			return nil
		}
		return []preparedTaskChildBlock{{label: newLabel(".call_body"), tasks: data.BodyTasks}}
	default:
		return nil
	}
}

func formatPreparedTaskOperand(task runtime.Task) string {
	switch data := task.Data.(type) {
	case nil:
		return ""
	case string:
		return data
	case int:
		return strconv.Itoa(data)
	case runtime.SymbolRef:
		return formatSymbolRef(data)
	case *runtime.LoadVarData:
		if data == nil {
			return ""
		}
		if data.Sym.Name != "" || data.Sym.Kind != runtime.SymbolUnknown {
			return formatSymbolRef(data.Sym)
		}
		return data.Name
	case *runtime.LHSData:
		if data == nil {
			return ""
		}
		return formatLHSData(data)
	case *runtime.CallData:
		if data == nil {
			return ""
		}
		parts := []string{data.Name}
		parts = append(parts, fmt.Sprintf("argc=%d", data.ArgCount))
		if data.Ellipsis {
			parts = append(parts, "ellipsis")
		}
		if data.Sym.Name != "" || data.Sym.Kind != runtime.SymbolUnknown {
			parts = append(parts, formatSymbolRef(data.Sym))
		}
		return strings.Join(filterEmptyStrings(parts), " ")
	case *runtime.ImportInitData:
		if data == nil {
			return ""
		}
		return data.Path
	case *runtime.IndexData:
		if data == nil {
			return ""
		}
		parts := []string{}
		if data.Multi {
			parts = append(parts, "multi")
		}
		if !data.ResultType.IsEmpty() {
			parts = append(parts, data.ResultType.String())
		}
		return strings.Join(parts, " ")
	case *runtime.SliceData:
		if data == nil {
			return ""
		}
		return fmt.Sprintf("low=%t high=%t", data.HasLow, data.HasHigh)
	case *runtime.AssertData:
		if data == nil {
			return ""
		}
		parts := []string{data.TargetType.String()}
		if data.Multi {
			parts = append(parts, "multi")
		}
		if !data.ResultType.IsEmpty() {
			parts = append(parts, "result="+data.ResultType.String())
		}
		return strings.Join(parts, " ")
	case *runtime.CompositeData:
		if data == nil {
			return ""
		}
		return fmt.Sprintf("%s entries=%d", data.Type, len(data.Entries))
	case *runtime.Var:
		return formatRuntimeVarInline(data)
	case *runtime.DoCallData:
		if data == nil {
			return ""
		}
		return fmt.Sprintf("%s %s argc=%d", data.Name, data.FunctionSig.SignatureString(), len(data.Args))
	case *runtime.DeclareVarData:
		if data == nil {
			return ""
		}
		parts := []string{data.Name}
		if !data.Kind.IsEmpty() {
			parts = append(parts, data.Kind.String())
		}
		if data.Sym.Name != "" || data.Sym.Kind != runtime.SymbolUnknown {
			parts = append(parts, formatSymbolRef(data.Sym))
		}
		return strings.Join(parts, " ")
	case *runtime.ClosureData:
		if data == nil {
			return ""
		}
		return fmt.Sprintf("%s captures=%d", data.FunctionSig.SignatureString(), len(data.CaptureRefs))
	default:
		return ""
	}
}

func preparedTaskComment(task runtime.Task, children []preparedTaskChildBlock) string {
	parts := make([]string, 0, 3)
	switch data := task.Data.(type) {
	case *runtime.BranchData:
		parts = append(parts, formatChildTargets(children))
	case *runtime.ForData:
		parts = append(parts, formatChildTargets(children))
	case *runtime.DeferData:
		parts = append(parts, fmt.Sprintf("%s | pop_result=%t", formatChildTargets(children), data.PopResult))
	case *runtime.FinallyData:
		parts = append(parts, formatChildTargets(children))
	case *runtime.CatchData:
		parts = append(parts, fmt.Sprintf("var=%s | %s", data.VarName, formatChildTargets(children)))
	case *runtime.RangeData:
		parts = append(parts, fmt.Sprintf("define=%t key=%s value=%s | %s", data.Define, data.Key, data.Value, formatChildTargets(children)))
	case *runtime.SwitchData:
		parts = append(parts, formatChildTargets(children))
	case *runtime.JumpData:
		parts = append(parts, fmt.Sprintf("op=%s | %s", data.Operator, formatChildTargets(children)))
	case *runtime.DoCallData:
		parts = append(parts, formatChildTargets(children))
	}
	if task.Source != nil {
		if task.Source.ID != "" {
			parts = append(parts, "node="+task.Source.ID)
		}
		if task.Source.File != "" {
			parts = append(parts, fmt.Sprintf("%s:%d:%d", task.Source.File, task.Source.Line, task.Source.Col))
		} else if task.Source.Line > 0 {
			parts = append(parts, fmt.Sprintf("L%d:%d", task.Source.Line, task.Source.Col))
		}
	}
	return strings.Join(filterEmptyStrings(parts), " | ")
}

func formatChildTargets(children []preparedTaskChildBlock) string {
	if len(children) == 0 {
		return ""
	}
	parts := make([]string, 0, len(children))
	for _, child := range children {
		role := child.label
		if idx := strings.LastIndex(role, ".L"); idx >= 0 {
			role = role[idx+6:]
		}
		role = strings.TrimPrefix(role, ".")
		parts = append(parts, fmt.Sprintf("%s->%s", role, child.label))
	}
	return strings.Join(parts, " ")
}

func formatSymbolRef(sym runtime.SymbolRef) string {
	kind := "unknown"
	switch sym.Kind {
	case runtime.SymbolGlobal:
		kind = "global"
	case runtime.SymbolLocal:
		kind = "local"
	case runtime.SymbolUpvalue:
		kind = "upvalue"
	case runtime.SymbolBuiltin:
		kind = "builtin"
	}
	if sym.Name == "" {
		return fmt.Sprintf("%s[%d]", kind, sym.Slot)
	}
	return fmt.Sprintf("%s[%d]=%s", kind, sym.Slot, sym.Name)
}

func formatLHSData(data *runtime.LHSData) string {
	parts := make([]string, 0, 3)
	switch data.Kind {
	case runtime.LHSTypeEnv:
		parts = append(parts, "env")
	case runtime.LHSTypeIndex:
		parts = append(parts, "index")
	case runtime.LHSTypeMember:
		parts = append(parts, "member")
	case runtime.LHSTypeStar:
		parts = append(parts, "deref")
	default:
		parts = append(parts, "none")
	}
	if data.Name != "" {
		parts = append(parts, data.Name)
	}
	if data.Property != "" {
		parts = append(parts, "."+data.Property)
	}
	if data.Sym.Name != "" || data.Sym.Kind != runtime.SymbolUnknown {
		parts = append(parts, formatSymbolRef(data.Sym))
	}
	return strings.Join(parts, " ")
}

func filterEmptyStrings(items []string) []string {
	out := items[:0]
	for _, item := range items {
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func formatRuntimeVarInline(v *runtime.Var) string {
	if v == nil {
		return "nil"
	}
	switch v.VType {
	case runtime.TypeInt:
		return strconv.FormatInt(v.I64, 10)
	case runtime.TypeFloat:
		return fmt.Sprintf("%g", v.F64)
	case runtime.TypeString:
		return fmt.Sprintf("%q", v.Str)
	case runtime.TypeBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case runtime.TypeBytes:
		return fmt.Sprintf("bytes[%d]", len(v.B))
	case runtime.TypeHandle:
		return fmt.Sprintf("handle(%d)", v.Handle)
	case runtime.TypeArray:
		if arr, ok := v.Ref.(*runtime.VMArray); ok && arr != nil {
			return fmt.Sprintf("array(len=%d)", len(arr.Data))
		}
		return "array"
	case runtime.TypeMap:
		if m, ok := v.Ref.(*runtime.VMMap); ok && m != nil {
			return fmt.Sprintf("map(len=%d)", len(m.Data))
		}
		return "map"
	case runtime.TypeClosure:
		return "closure"
	case runtime.TypeModule:
		return "module"
	case runtime.TypeCell:
		return "cell"
	case runtime.TypeAny:
		return "any"
	case runtime.TypeInterface:
		return "interface"
	case runtime.TypeError:
		if v.Str != "" {
			return fmt.Sprintf("error(%q)", v.Str)
		}
		return "error"
	default:
		if !v.RuntimeType().IsEmpty() {
			return v.RuntimeType().String()
		}
		return "nil"
	}
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
	return fn.FunctionSig.SignatureString()
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
