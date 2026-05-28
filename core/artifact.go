package engine

import (
	"encoding/json"
	"errors"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/compiler"
)

type ExecutableArtifact struct {
	Filename string
	Source   string
	Bytecode *bytecode.Program
}

type AnalysisArtifact struct {
	Filename         string
	Source           string
	Program          *ast.ProgramStmt
	ImportedPrograms map[string]*ast.ProgramStmt
	TemplatePreviews []calltemplate.TemplatePreview
}

func executableArtifactFromCompiled(compiled *compiler.Artifact) (*ExecutableArtifact, error) {
	if compiled == nil {
		return nil, errors.New("invalid compiled program")
	}
	return ExecutableArtifactFromBytecode(compiled.Filename, compiled.Source, compiled.Bytecode)
}

func ExecutableArtifactFromBytecode(filename, source string, program *bytecode.Program) (*ExecutableArtifact, error) {
	if program == nil {
		return nil, errors.New("invalid bytecode program")
	}
	if err := program.Validate(); err != nil {
		return nil, err
	}
	if filename == "" {
		filename = "bytecode"
	}
	return &ExecutableArtifact{
		Filename: filename,
		Source:   source,
		Bytecode: program,
	}, nil
}

func ExecutableArtifactFromBytecodeJSON(payload []byte) (*ExecutableArtifact, error) {
	program, err := bytecode.UnmarshalJSON(payload)
	if err != nil {
		return nil, err
	}
	return ExecutableArtifactFromBytecode("bytecode", "", program)
}

func analysisArtifactFromCompiled(source string, compiled *compiler.Artifact, program *ast.ProgramStmt) *AnalysisArtifact {
	if program == nil && compiled != nil {
		program = compiled.Program
	}
	artifact := &AnalysisArtifact{
		Source:  source,
		Program: program,
	}
	if compiled != nil {
		artifact.Filename = compiled.Filename
		if artifact.Source == "" {
			artifact.Source = compiled.Source
		}
		artifact.ImportedPrograms = cloneProgramMap(compiled.ImportedPrograms)
		artifact.TemplatePreviews = append([]calltemplate.TemplatePreview(nil), compiled.TemplatePreviews...)
	}
	return artifact
}

func cloneProgramMap(in map[string]*ast.ProgramStmt) map[string]*ast.ProgramStmt {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*ast.ProgramStmt, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (a *ExecutableArtifact) MarshalJSON() ([]byte, error) {
	return a.MarshalBytecodeJSON()
}

func (a *ExecutableArtifact) MarshalIndentJSON(prefix, indent string) ([]byte, error) {
	return a.MarshalIndentBytecodeJSON(prefix, indent)
}

func (a *ExecutableArtifact) MarshalBytecodeJSON() ([]byte, error) {
	return marshalExecutableArtifactBytecode(a, "", "")
}

func (a *ExecutableArtifact) MarshalIndentBytecodeJSON(prefix, indent string) ([]byte, error) {
	return marshalExecutableArtifactBytecode(a, prefix, indent)
}

func (a *ExecutableArtifact) Disassemble() string {
	if a == nil || a.Bytecode == nil {
		return "; Error: invalid or uninitialized bytecode artifact\n"
	}
	return a.Bytecode.Disassemble()
}

func marshalExecutableArtifactBytecode(a *ExecutableArtifact, prefix, indent string) ([]byte, error) {
	if a == nil || a.Bytecode == nil {
		return nil, errors.New("cannot marshal empty bytecode artifact")
	}
	if indent == "" && prefix == "" {
		return json.Marshal(a.Bytecode)
	}
	return json.MarshalIndent(a.Bytecode, prefix, indent)
}
