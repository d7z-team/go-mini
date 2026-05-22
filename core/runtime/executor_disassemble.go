package runtime

import (
	"fmt"
	"sort"
	"strings"
)

func (e *Executor) Disassemble() (res string) {
	defer func() {
		if r := recover(); r != nil {
			res = fmt.Sprintf("; Disassembly failed: %v\n", r)
		}
	}()

	var sb strings.Builder
	sb.WriteString("; Go-Mini VM Disassembly\n")
	fmt.Fprintf(&sb, "; Total Variables: %d\n", len(e.globals))
	fmt.Fprintf(&sb, "; Total Functions: %d\n\n", len(e.functions))

	sb.WriteString("section .data:\n")
	globalKeys := make([]string, 0, len(e.globals))
	for name := range e.globals {
		globalKeys = append(globalKeys, name)
	}
	sort.Strings(globalKeys)
	for _, key := range globalKeys {
		global := e.globals[key]
		fmt.Fprintf(&sb, "  global %s\n", key)
		fmt.Fprintf(&sb, "global.%s:\n", key)
		if global != nil {
			e.disassembleTasks(&sb, "  ", global.InitPlan)
		}
	}
	sb.WriteString("\n")

	sb.WriteString("section .text:\n")
	if len(e.mainTasks) > 0 {
		sb.WriteString("global _start\n")
		sb.WriteString("_start:\n")
		e.disassembleTasks(&sb, "  ", e.mainTasks)
		sb.WriteString("\n")
	}

	keys := make([]string, 0, len(e.functions))
	for k := range e.functions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		f := e.functions[k]
		sig := FuncType(nil, SpecVoid, false).String()
		var bodyTasks []Task
		if f != nil && f.FunctionSig != nil {
			sig = string(f.FunctionSig.Spec)
		}
		if f != nil {
			bodyTasks = f.BodyTasks
		}
		fmt.Fprintf(&sb, "fn.%s: ; signature %s\n", k, sig)
		e.disassembleTasks(&sb, "  ", bodyTasks)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (e *Executor) disassembleTasks(sb *strings.Builder, indent string, tasks []Task) {
	defer func() {
		if r := recover(); r != nil {
			sb.WriteString(indent + "; Disassembly failed for this task plan: " + fmt.Sprintf("%v", r) + "\n")
		}
	}()

	if len(tasks) == 0 {
		return
	}

	queue := cloneTasks(tasks)

	for len(queue) > 0 {
		task := queue[len(queue)-1]
		queue = queue[:len(queue)-1]

		dataStr := ""
		if task.Data != nil {
			if v, ok := task.Data.(*Var); ok && v != nil {
				if v.VType == TypeString {
					dataStr = fmt.Sprintf("%q", v.Str)
				} else {
					dataStr = v.String()
				}
			} else if env, ok := task.Data.(*LHSEnv); ok {
				dataStr = env.Name
			} else if mem, ok := task.Data.(*LHSMember); ok {
				objStr := "nil"
				if mem.Obj != nil {
					objStr = mem.Obj.String()
				}
				dataStr = fmt.Sprintf("%v.%v", objStr, mem.Property)
			} else if _, ok := task.Data.(*LHSIndex); ok {
				dataStr = "[]"
			} else if ld, ok := task.Data.(*LHSData); ok {
				switch ld.Kind {
				case LHSTypeEnv:
					dataStr = ld.Name
				case LHSTypeMember:
					dataStr = ld.Property
				case LHSTypeIndex:
					dataStr = "[]"
				case LHSTypeStar:
					dataStr = "*"
				}
			} else if cd, ok := task.Data.(*CallData); ok {
				dataStr = cd.Name
			} else if ld, ok := task.Data.(*LoadVarData); ok {
				dataStr = ld.Name
			} else if sym, ok := task.Data.(SymbolRef); ok {
				dataStr = fmt.Sprintf("%s[%d]", sym.Name, sym.Slot)
			} else {
				dataStr = fmt.Sprintf("%v", task.Data)
			}
		}
		dataStr = strings.ReplaceAll(dataStr, "\n", "\\n")
		dataStr = strings.ReplaceAll(dataStr, "\r", "\\r")

		addr := "[                ]"
		comment := ""
		if task.Source != nil {
			addr = fmt.Sprintf("[%16s]", task.Source.ID)
			comment = task.Source.Meta

			if task.Source.Line > 0 {
				comment = fmt.Sprintf("%s at L%d:%d", comment, task.Source.Line, task.Source.Col)
			}
		}

		switch task.Op {
		case OpCall:
			if cd, ok := task.Data.(*CallData); ok {
				comment = "Call " + cd.Name
			}
		case OpLoadLocal, OpLoadUpvalue:
			if sym, ok := task.Data.(SymbolRef); ok {
				comment = fmt.Sprintf("Load %s slot %d", sym.Name, sym.Slot)
			}
		case OpStoreLocal, OpStoreUpvalue:
			if sym, ok := task.Data.(SymbolRef); ok {
				comment = fmt.Sprintf("Store %s slot %d", sym.Name, sym.Slot)
			}
		case OpAssign:
			comment = "Assignment"
		case OpReturn:
			comment = fmt.Sprintf("Return %v values", task.Data)
		case OpJumpIf:
			if jd, ok := task.Data.(*JumpData); ok {
				comment = fmt.Sprintf("Short-circuit %v", jd.Operator)
			}
		case OpPush:
			comment = "Literal value"
		case OpLoadVar:
			switch data := task.Data.(type) {
			case *LoadVarData:
				comment = fmt.Sprintf("Load variable '%v'", data.Name)
			default:
				comment = fmt.Sprintf("Load variable '%v'", task.Data)
			}
		case OpPop:
			comment = "Pop stack"
		}

		line := fmt.Sprintf("%s  %-18s %-22s", addr, task.Op.String(), dataStr)
		if comment != "" {
			line = fmt.Sprintf("%-65s ; %s", line, comment)
		}
		sb.WriteString(indent + line + "\n")
	}
}
