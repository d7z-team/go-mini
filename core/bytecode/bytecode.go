package bytecode

import (
	"fmt"
	"strings"
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
	Globals   []Global      `json:"globals"`
	Entry     []Instruction `json:"entry"`
	Functions []Function    `json:"functions"`
}

func (p *Program) Disassemble() string {
	if p == nil {
		return "; Error: no bytecode loaded\n"
	}

	var sb strings.Builder
	sb.WriteString("; Go-Mini Bytecode Disassembly\n")
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
