package semantic

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/source"
)

type variableBinding struct {
	objects []ObjectID
	values  []ast.Expression
	scope   ScopeID
	span    source.Span
	state   uint8
}

const (
	variableEvaluating uint8 = iota + 1
	variableDone
	variableFailed
)

func (a *analyzer) indexPackageVariables(program ast.Program) {
	for i := range program.Files {
		file := &program.Files[i]
		scope := a.files[file.Path]
		for j := range file.Decls {
			decl := &file.Decls[j]
			if decl.Kind != ast.DeclVar || len(decl.Var.Values) == 0 {
				continue
			}
			binding := &variableBinding{
				values: append([]ast.Expression(nil), decl.Var.Values...),
				scope:  scope,
				span:   decl.Span,
			}
			binding.objects = make([]ObjectID, len(decl.Var.Names))
			for index, name := range decl.Var.Names {
				object, ok := a.info.Lookup(scope, strings.TrimSpace(name))
				if ok && object.Kind == ObjectVar && object.Node == decl.NodeID && !object.Type.Valid() {
					binding.objects[index] = object.ID
				}
			}
			for _, id := range binding.objects {
				if id != "" {
					a.variableBindings[id] = binding
				}
			}
		}
	}
}

func (a *analyzer) inferVariableObject(id ObjectID) {
	binding, ok := a.variableBindings[id]
	if !ok {
		return
	}
	if binding.state == variableDone || binding.state == variableFailed {
		return
	}
	if binding.state == variableEvaluating {
		a.addDiagnostic("semantic.var.cycle", "variable initialization cycle", binding.span)
		binding.state = variableFailed
		return
	}
	binding.state = variableEvaluating
	for index := range binding.values {
		a.analyzeExpr(&binding.values[index], binding.scope)
	}
	if binding.state == variableFailed {
		return
	}
	inferred := a.assignmentExpressionTypes(binding.values, len(binding.objects))
	if len(inferred) != len(binding.objects) {
		a.addDiagnostic("semantic.decl.value_count", "declaration value count does not match name count", binding.span)
		binding.state = variableFailed
		return
	}
	for _, typ := range inferred {
		if !typ.Valid() {
			binding.state = variableFailed
			return
		}
	}
	for index, objectID := range binding.objects {
		if objectID == "" {
			continue
		}
		object := a.info.Objects[objectID]
		object.Type = inferred[index]
		object.Untyped = false
		a.info.Objects[objectID] = object
	}
	binding.state = variableDone
}
