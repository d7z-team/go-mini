package bytecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.d7z.net/go-mini/core/runtime"
)

const (
	FormatGoMiniBytecode = "go-mini-bytecode"
	CurrentVersion       = 12
	pseudoOpLabel        = "PSEUDO_LABEL"
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
	Executable *runtime.PreparedProgram `json:"executable,omitempty"`
}

func NewProgram() *Program {
	return &Program{
		Format:    FormatGoMiniBytecode,
		Version:   CurrentVersion,
		OpcodeSet: "runtime.opcode.v5",
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
	if p.Executable != nil {
		if err := validateNoEmbeddedSourceModules(p.Executable); err != nil {
			return err
		}
		if err := runtime.ValidatePreparedProgram(p.Executable); err != nil {
			return fmt.Errorf("invalid executable bytecode: %w", err)
		}
	}
	return nil
}

func (p *Program) MarshalJSON() ([]byte, error) {
	if p != nil && p.Executable != nil {
		if err := validateNoEmbeddedSourceModules(p.Executable); err != nil {
			return nil, err
		}
	}
	type programJSON Program
	return json.Marshal((*programJSON)(p))
}

func (p *Program) UnmarshalJSON(data []byte) error {
	if p == nil {
		return errors.New("nil bytecode program")
	}
	if err := rejectEmbeddedModulePayload(data); err != nil {
		return err
	}
	type programJSON Program
	var decoded programJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	program := Program(decoded)
	if err := program.Validate(); err != nil {
		return err
	}
	*p = program
	return nil
}

func validateNoEmbeddedSourceModules(prepared *runtime.PreparedProgram) error {
	if prepared == nil {
		return nil
	}
	if len(prepared.Modules) > 0 || len(prepared.ModuleHashes) > 0 {
		return errors.New("bytecode executable must not embed source modules")
	}
	return nil
}

func UnmarshalJSON(data []byte) (*Program, error) {
	var program Program
	if err := json.Unmarshal(data, &program); err != nil {
		return nil, err
	}
	return &program, nil
}

func rejectEmbeddedModulePayload(data []byte) error {
	var raw struct {
		Executable json.RawMessage `json:"executable"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Executable) == 0 || string(raw.Executable) == "null" {
		return nil
	}
	var executable map[string]json.RawMessage
	if err := json.Unmarshal(raw.Executable, &executable); err != nil {
		return err
	}
	if _, ok := executable["modules"]; ok {
		return errors.New("bytecode executable must not embed source modules")
	}
	if _, ok := executable["module_hashes"]; ok {
		return errors.New("bytecode executable must not embed source module hashes")
	}
	return nil
}

func (p *Program) Disassemble() string {
	if p == nil {
		return "; Error: no bytecode loaded\n"
	}

	var sb strings.Builder
	sb.WriteString("; go-mini bytecode disassembly\n")
	fmt.Fprintf(&sb, "; format     %s v%d\n", p.Format, p.Version)
	fmt.Fprintf(&sb, "; opcode_set %s\n", p.OpcodeSet)
	if p.Executable != nil && p.Executable.Package != "" {
		fmt.Fprintf(&sb, "; package    %s\n", p.Executable.Package)
	}
	execGlobals := 0
	execFunctions := 0
	if p.Executable != nil {
		execGlobals = len(p.Executable.Globals)
		execFunctions = len(p.Executable.Functions)
	}
	fmt.Fprintf(&sb, "; globals    display=%d executable=%d\n", len(p.Globals), execGlobals)
	fmt.Fprintf(&sb, "; functions  display=%d executable=%d\n", len(p.Functions), execFunctions)
	sb.WriteString("\n")

	writeExecutableMetadataSection(&sb, p)
	writeGlobalsSections(&sb, p)
	writeTextSection(&sb, p)

	return sb.String()
}

func (p *Program) RefreshDisplayFromExecutable() {
	if p == nil || p.Executable == nil {
		return
	}
	p.Entry = instructionsFromPreparedTasks(p.Executable.MainTasks, "_start")

	blocks := preparedGlobalInitBlocks(p.Executable)
	p.Globals = make([]Global, 0, len(blocks))
	for _, block := range blocks {
		p.Globals = append(p.Globals, Global{
			Name:         block.name,
			Instructions: instructionsFromPreparedTasks(block.tasks, "global."+sanitizeLabel(block.name)),
		})
	}

	names := sortedPreparedFunctionNames(p.Executable.Functions)
	p.Functions = make([]Function, 0, len(names))
	for _, name := range names {
		fn := p.Executable.Functions[name]
		p.Functions = append(p.Functions, Function{
			Name:         name,
			Signature:    formatExecutableSignature(fn),
			Instructions: instructionsFromPreparedTasks(fn.BodyTasks, "fn."+sanitizeLabel(name)),
		})
	}
}

func writeExecutableMetadataSection(sb *strings.Builder, p *Program) {
	if p == nil || p.Executable == nil {
		return
	}

	if len(p.Executable.NamedTypes) > 0 || len(p.Executable.StructSchemas) > 0 || len(p.Executable.InterfaceSchemas) > 0 {
		sb.WriteString("section .note.go_mini\n")
		writeExecutableMetadataNotes(sb, p)
		sb.WriteString("\n")
	}
}

func writeGlobalsSections(sb *strings.Builder, p *Program) {
	var execGlobals map[string]*runtime.PreparedGlobal
	var execBlocks []executableGlobalInitBlock
	if p != nil && p.Executable != nil {
		execGlobals = p.Executable.Globals
		execBlocks = preparedGlobalInitBlocks(p.Executable)
	}
	var globalsWithInitBlock map[string]struct{}
	if len(execBlocks) > 0 {
		globalsWithInitBlock = make(map[string]struct{})
		for _, block := range execBlocks {
			for _, name := range block.names {
				globalsWithInitBlock[name] = struct{}{}
			}
		}
	}
	uninitialized := make([]string, 0)
	for name, global := range execGlobals {
		if global == nil || global.HasInit || len(global.InitPlan) > 0 {
			continue
		}
		if _, hasInitBlock := globalsWithInitBlock[name]; hasInitBlock {
			continue
		}
		uninitialized = append(uninitialized, name)
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
	if p != nil && p.Executable != nil {
		if len(execBlocks) == 0 {
			sb.WriteString("; no global initializers\n\n")
			return
		}
		writePreparedGlobalInitBlocks(sb, execBlocks, "global", "")
		return
	}

	initGlobals := append([]Global(nil), p.Globals...)
	sort.Slice(initGlobals, func(i, j int) bool {
		return initGlobals[i].Name < initGlobals[j].Name
	})
	if len(initGlobals) == 0 {
		sb.WriteString("; no global initializers\n\n")
		return
	}
	for _, global := range initGlobals {
		fmt.Fprintf(sb, "global.%s:\n", sanitizeLabel(global.Name))
		writeInstructions(sb, global.Instructions)
		sb.WriteString("\n")
	}
}

func writeTextSection(sb *strings.Builder, p *Program) {
	sb.WriteString("section .text\n")
	sb.WriteString("global _start\n\n")
	sb.WriteString("_start:\n")
	if p != nil && p.Executable != nil {
		writePreparedTasks(sb, p.Executable.MainTasks, "_start")
		sb.WriteString("\n")
		writePreparedFunctions(sb, p.Executable.Functions, "fn", "")
		return
	}

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
	sort.Strings(names)

	for _, name := range names {
		fn := displayFunctions[name]
		if fn.Signature != "" {
			fmt.Fprintf(sb, "fn.%s: ; signature %s\n", sanitizeLabel(name), fn.Signature)
		} else {
			fmt.Fprintf(sb, "fn.%s:\n", sanitizeLabel(name))
		}
		if len(fn.Instructions) > 0 {
			writeInstructions(sb, fn.Instructions)
		} else {
			sb.WriteString("    ; no body\n")
		}
		sb.WriteString("\n")
	}
}

func writePreparedGlobalInitBlocks(sb *strings.Builder, blocks []executableGlobalInitBlock, labelPrefix, linePrefix string) {
	for _, block := range blocks {
		scope := labelPrefix + "." + sanitizeLabel(block.name)
		fmt.Fprintf(sb, "%s%s: ; names=%s\n", linePrefix, scope, strings.Join(block.names, ","))
		writePreparedTasks(sb, block.tasks, scope)
		sb.WriteString("\n")
	}
}

func writePreparedFunctions(sb *strings.Builder, functions map[string]*runtime.PreparedFunction, labelPrefix, linePrefix string) {
	for _, name := range sortedPreparedFunctionNames(functions) {
		fn := functions[name]
		scope := labelPrefix + "." + sanitizeLabel(name)
		if sig := formatExecutableSignature(fn); sig != "" {
			fmt.Fprintf(sb, "%s%s: ; signature %s\n", linePrefix, scope, sig)
		} else {
			fmt.Fprintf(sb, "%s%s:\n", linePrefix, scope)
		}
		writePreparedTasks(sb, fn.BodyTasks, scope)
		sb.WriteString("\n")
	}
}

func instructionsFromPreparedTasks(tasks []runtime.Task, scope string) []Instruction {
	if len(tasks) == 0 {
		return []Instruction{}
	}
	state := &preparedDisasmState{}
	return appendPreparedInstructions([]Instruction{}, tasks, scope, state)
}

func appendPreparedInstructions(out []Instruction, tasks []runtime.Task, scope string, state *preparedDisasmState) []Instruction {
	for i := len(tasks) - 1; i >= 0; i-- {
		task := tasks[i]
		if !shouldDisplayPreparedTask(task) {
			continue
		}
		children := preparedTaskChildren(task, scope, state)
		operand := formatPreparedTaskOperandForDisplay(task, state)
		out = append(out, instructionFromPreparedTask(task, operand, children))
		for _, child := range children {
			out = append(out, Instruction{
				Op:      pseudoOpLabel,
				Operand: child.label,
				Comment: "block",
			})
			out = appendPreparedInstructions(out, child.tasks, child.label, state)
		}
	}
	return out
}

func instructionFromPreparedTask(task runtime.Task, operand string, children []preparedTaskChildBlock) Instruction {
	inst := Instruction{
		Op:      task.Op.String(),
		Operand: operand,
		Comment: preparedTaskComment(task, children),
	}
	if task.Source != nil {
		inst.NodeID = task.Source.ID
		if task.Source.File != "" || task.Source.Line > 0 || task.Source.Col > 0 {
			inst.Loc = &Location{
				File:   task.Source.File,
				Line:   task.Source.Line,
				Column: task.Source.Col,
			}
		}
	}
	return inst
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
	nextLabel  int
	scopeStack []string
}

func writePreparedTasks(sb *strings.Builder, tasks []runtime.Task, scope string) {
	pc := 0
	state := &preparedDisasmState{}
	writePreparedTaskBlock(sb, tasks, "    ", scope, &pc, state)
}

func writePreparedTaskBlock(sb *strings.Builder, tasks []runtime.Task, indent, scope string, pc *int, state *preparedDisasmState) {
	if len(tasks) == 0 {
		sb.WriteString(indent + "; no body\n")
		return
	}
	wrote := false
	for i := len(tasks) - 1; i >= 0; i-- {
		task := tasks[i]
		if !shouldDisplayPreparedTask(task) {
			continue
		}
		wrote = true
		children := preparedTaskChildren(task, scope, state)
		operand := formatPreparedTaskOperandForDisplay(task, state)
		line := fmt.Sprintf("%s%04d  %-18s %s", indent, *pc, task.Op.String(), operand)
		comment := preparedTaskComment(task, children)
		if comment != "" {
			line = fmt.Sprintf("%-64s ; %s", line, comment)
		}
		sb.WriteString(strings.TrimRight(line, " ") + "\n")
		*pc++
		for _, child := range children {
			sb.WriteString(indent + child.label + ":\n")
			writePreparedTaskBlock(sb, child.tasks, indent+"    ", child.label, pc, state)
		}
	}
	if !wrote {
		sb.WriteString(indent + "; no instructions\n")
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
		blocks := make([]preparedTaskChildBlock, 0, 1+len(data.Cases)*2)
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
	case *runtime.SelectData:
		blocks := make([]preparedTaskChildBlock, 0, len(data.Cases))
		for idx, c := range data.Cases {
			if len(c.Body) == 0 {
				continue
			}
			blocks = append(blocks, preparedTaskChildBlock{
				label: newLabel(fmt.Sprintf(".select_case_%d_%s", idx, c.Kind)),
				tasks: c.Body,
			})
		}
		return blocks
	default:
		return nil
	}
}

func shouldDisplayPreparedTask(task runtime.Task) bool {
	switch task.Op {
	case runtime.OpLineStep:
		return task.Source != nil && (task.Source.ID != "" || task.Source.File != "" || task.Source.Line > 0 || task.Source.Col > 0)
	case runtime.OpEvalLHS:
		if task.Data == nil {
			return false
		}
		data, ok := task.Data.(*runtime.LHSData)
		return !ok || data == nil || data.Kind != runtime.LHSTypeNone
	default:
		return true
	}
}

func formatPreparedTaskOperandForDisplay(task runtime.Task, state *preparedDisasmState) string {
	operand := formatPreparedTaskOperand(task)
	switch task.Op {
	case runtime.OpScopeEnter, runtime.OpForScopeEnter, runtime.OpRangeScopeEnter, runtime.OpSelectScopeEnter, runtime.OpCatchScopeEnter:
		name := operand
		if name == "" {
			switch task.Op {
			case runtime.OpForScopeEnter:
				name = "for_body"
			case runtime.OpRangeScopeEnter:
				name = "for_range_body"
			case runtime.OpSelectScopeEnter:
				name = "select_case"
			case runtime.OpCatchScopeEnter:
				name = "catch"
			default:
				name = "scope"
			}
		}
		state.scopeStack = append(state.scopeStack, name)
	case runtime.OpScopeExit, runtime.OpForScopeExit:
		if len(state.scopeStack) > 0 {
			operand = state.scopeStack[len(state.scopeStack)-1]
			state.scopeStack = state.scopeStack[:len(state.scopeStack)-1]
		}
	}
	return operand
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
	case runtime.RuntimeType:
		return data.String()
	case *runtime.Var:
		return formatRuntimeVarInline(data)
	case *runtime.DoCallData:
		if data == nil {
			return ""
		}
		parts := []string{data.Name}
		if data.FunctionSig != nil {
			parts = append(parts, data.FunctionSig.SignatureString())
		}
		parts = append(parts, fmt.Sprintf("argc=%d", len(data.Args)))
		return strings.Join(filterEmptyStrings(parts), " ")
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
	case *runtime.VarDeclData:
		if data == nil {
			return ""
		}
		names := make([]string, 0, len(data.Bindings))
		for _, binding := range data.Bindings {
			if binding.Name != "" {
				names = append(names, binding.Name)
			}
		}
		parts := []string{
			string(data.Mode),
			fmt.Sprintf("bindings=%d", len(data.Bindings)),
			fmt.Sprintf("values=%d", data.ValueCount),
		}
		if len(names) > 0 {
			parts = append(parts, "names="+strings.Join(names, ","))
		}
		return strings.Join(filterEmptyStrings(parts), " ")
	case *runtime.MultiAssignData:
		if data == nil {
			return ""
		}
		return strings.Join(filterEmptyStrings([]string{
			string(data.Mode),
			fmt.Sprintf("lhs=%d", data.LHSCount),
			fmt.Sprintf("values=%d", data.ValueCount),
		}), " ")
	case *runtime.ClosureData:
		if data == nil {
			return ""
		}
		parts := make([]string, 0, 2)
		if data.FunctionSig != nil {
			parts = append(parts, data.FunctionSig.SignatureString())
		}
		parts = append(parts, fmt.Sprintf("captures=%d", len(data.CaptureRefs)))
		return strings.Join(filterEmptyStrings(parts), " ")
	case *runtime.MemberData:
		if data == nil {
			return ""
		}
		parts := []string{data.Property}
		if !data.ObjectType.IsEmpty() {
			parts = append(parts, "object="+data.ObjectType.String())
		}
		return strings.Join(filterEmptyStrings(parts), " ")
	case *runtime.ChanRecvData:
		if data == nil {
			return ""
		}
		parts := make([]string, 0, 2)
		if data.Multi {
			parts = append(parts, "multi")
		}
		if !data.ResultType.IsEmpty() {
			parts = append(parts, "result="+data.ResultType.String())
		}
		return strings.Join(parts, " ")
	case *runtime.RangeData:
		if data == nil {
			return ""
		}
		parts := []string{}
		if data.Define {
			parts = append(parts, "define")
		}
		if data.Key != "" {
			parts = append(parts, "key="+data.Key)
		}
		if data.Value != "" {
			parts = append(parts, "value="+data.Value)
		}
		if !data.KeyType.IsEmpty() {
			parts = append(parts, "key_type="+data.KeyType.String())
		}
		if !data.ValType.IsEmpty() {
			parts = append(parts, "value_type="+data.ValType.String())
		}
		return strings.Join(parts, " ")
	case *runtime.SwitchData:
		if data == nil {
			return ""
		}
		parts := []string{fmt.Sprintf("cases=%d", len(data.Cases))}
		if data.IsType {
			parts = append(parts, "type")
		}
		if data.HasTag {
			parts = append(parts, "tag")
		}
		if data.HasAssign {
			parts = append(parts, "assign")
		}
		if len(data.DefaultBody) > 0 {
			parts = append(parts, "default")
		}
		return strings.Join(parts, " ")
	case *runtime.SelectData:
		if data == nil {
			return ""
		}
		return fmt.Sprintf("cases=%d", len(data.Cases))
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
	case *runtime.SelectData:
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
	case runtime.TypePointer:
		if v.Ref == nil {
			return "ptr(nil)"
		}
		return "ptr"
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
	case runtime.TypeAny:
		return "any"
	case runtime.TypeInterface:
		return "interface"
	case runtime.TypeStruct:
		return "struct"
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

func writeExecutableMetadataNotes(sb *strings.Builder, p *Program) {
	if p == nil || p.Executable == nil {
		return
	}
	typeKeys := make([]string, 0, len(p.Executable.NamedTypes))
	for name := range p.Executable.NamedTypes {
		typeKeys = append(typeKeys, name)
	}
	sort.Strings(typeKeys)
	for _, name := range typeKeys {
		fmt.Fprintf(sb, "; type %s = %s\n", name, p.Executable.NamedTypes[name].Raw)
	}

	structKeys := make([]string, 0, len(p.Executable.StructSchemas))
	for name := range p.Executable.StructSchemas {
		structKeys = append(structKeys, name)
	}
	sort.Strings(structKeys)
	for _, name := range structKeys {
		spec := p.Executable.StructSchemas[name]
		if spec == nil {
			fmt.Fprintf(sb, "; struct %s\n", name)
			continue
		}
		fields := make([]string, 0, len(spec.Fields))
		for _, field := range spec.Fields {
			fields = append(fields, fmt.Sprintf("%s %s", field.Name, field.Type))
		}
		fmt.Fprintf(sb, "; struct %s { %s }\n", name, strings.Join(fields, "; "))
	}

	ifaceKeys := make([]string, 0, len(p.Executable.InterfaceSchemas))
	for name := range p.Executable.InterfaceSchemas {
		ifaceKeys = append(ifaceKeys, name)
	}
	sort.Strings(ifaceKeys)
	for _, name := range ifaceKeys {
		spec := p.Executable.InterfaceSchemas[name]
		if spec == nil {
			fmt.Fprintf(sb, "; interface %s\n", name)
			continue
		}
		fmt.Fprintf(sb, "; interface %s %s\n", name, spec.Spec)
	}
}

type executableGlobalInitBlock struct {
	name  string
	names []string
	tasks []runtime.Task
}

func preparedGlobalInitBlocks(prepared *runtime.PreparedProgram) []executableGlobalInitBlock {
	if prepared == nil {
		return nil
	}
	if len(prepared.GlobalInitGroups) > 0 {
		blocks := make([]executableGlobalInitBlock, 0, len(prepared.GlobalInitGroups))
		for idx, group := range prepared.GlobalInitGroups {
			if group == nil || len(group.Names) == 0 {
				continue
			}
			name := strings.Join(group.Names, ",")
			if name == "" {
				name = fmt.Sprintf("group_%d", idx)
			}
			blocks = append(blocks, executableGlobalInitBlock{
				name:  name,
				names: append([]string(nil), group.Names...),
				tasks: group.InitPlan,
			})
		}
		return blocks
	}
	if len(prepared.Globals) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(prepared.Globals))
	blocks := make([]executableGlobalInitBlock, 0, len(prepared.Globals))
	add := func(name string) {
		if seen[name] {
			return
		}
		global := prepared.Globals[name]
		if global == nil || (!global.HasInit && len(global.InitPlan) == 0) {
			return
		}
		seen[name] = true
		blocks = append(blocks, executableGlobalInitBlock{name: name, names: []string{name}, tasks: global.InitPlan})
	}
	for _, name := range prepared.GlobalInitOrder {
		add(name)
	}
	remaining := make([]string, 0, len(prepared.Globals))
	for name := range prepared.Globals {
		if !seen[name] {
			remaining = append(remaining, name)
		}
	}
	sort.Strings(remaining)
	for _, name := range remaining {
		add(name)
	}
	return blocks
}

func formatExecutableSignature(fn *runtime.PreparedFunction) string {
	if fn == nil || fn.FunctionSig == nil {
		return ""
	}
	return fn.FunctionSig.SignatureString()
}

func sortedPreparedFunctionNames(functions map[string]*runtime.PreparedFunction) []string {
	names := make([]string, 0, len(functions))
	for name, fn := range functions {
		if fn != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func sanitizeLabel(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		".", "_",
		"-", "_",
		" ", "_",
		":", "_",
		",", "_",
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
