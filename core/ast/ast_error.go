package ast

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type MiniCodeError struct {
	Line    int
	Message string
}

func (e *MiniCodeError) Error() string {
	return fmt.Sprintf("在 L%d 出现错误", e.Line)
}

type MiniAstError struct {
	Err  error
	Logs []Logs
	Node Node
}

func (e *MiniAstError) Error() string {
	var decode bytes.Buffer
	decode.WriteString("\n验证失败:\n")
	if e.Node != nil {
		for _, log := range e.Logs {
			fmt.Fprintf(&decode, "  %v: %s\n", log.Path, log.Message)
		}
	}
	if e.Node != nil {
		decode.WriteString("\n\n")
		encoder := json.NewEncoder(&decode)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(e.Node)
	}
	return e.Err.Error() + decode.String()
}
