package runtime

import (
	"errors"

	"gopkg.d7z.net/go-mini/core/ast"
)

type PreparedProgram struct {
	GlobalInitOrder []ast.Ident                     `json:"global_init_order"`
	Globals         map[ast.Ident]*PreparedGlobal   `json:"globals"`
	Functions       map[ast.Ident]*PreparedFunction `json:"functions"`
	MainTasks       []Task                          `json:"main_tasks"`
}

type PreparedGlobal struct {
	Name     ast.Ident `json:"name"`
	HasInit  bool      `json:"has_init"`
	InitPlan []Task    `json:"init_plan,omitempty"`
}

type PreparedFunction struct {
	Name        ast.Ident       `json:"name"`
	FunctionSig *RuntimeFuncSig `json:"function_sig,omitempty"`
	BodyTasks   []Task          `json:"body_tasks,omitempty"`
}

func PrepareProgram(program *ast.ProgramStmt) (*PreparedProgram, error) {
	if program == nil {
		return nil, errors.New("invalid program")
	}

	exec := &Executor{
		consts: make(map[string]string),
	}
	for name, val := range program.Constants {
		exec.consts[name] = val
	}

	order, err := program.GlobalInitOrder()
	if err != nil {
		order = program.DeclaredGlobalOrder()
	}

	prepared := &PreparedProgram{
		GlobalInitOrder: append([]ast.Ident(nil), order...),
		Globals:         make(map[ast.Ident]*PreparedGlobal, len(program.Variables)),
		Functions:       make(map[ast.Ident]*PreparedFunction, len(program.Functions)),
	}

	rootScope := exec.newRootLoweringScope()
	for ident, expr := range program.Variables {
		item := &PreparedGlobal{
			Name:    ident,
			HasInit: expr != nil,
		}
		if expr != nil {
			item.InitPlan = exec.tasksForExprInScope(expr, rootScope)
		}
		prepared.Globals[ident] = item
	}
	for ident, fn := range program.Functions {
		if fn == nil {
			continue
		}
		fnScope := rootScope.childFunction()
		for _, p := range fn.Params {
			fnScope.declare(string(p.Name))
		}
		prepared.Functions[ident] = &PreparedFunction{
			Name:        ident,
			FunctionSig: MustRuntimeFuncSigFromFunction(fn.FunctionType),
			BodyTasks:   exec.tasksForStmtInScope(fn.Body, nil, fnScope),
		}
	}
	prepared.MainTasks = exec.buildStmtPlanWithScope(program.Main, rootScope)
	return prepared, nil
}

func clonePreparedProgram(plan *PreparedProgram) *PreparedProgram {
	if plan == nil {
		return nil
	}

	cloned := &PreparedProgram{
		GlobalInitOrder: append([]ast.Ident(nil), plan.GlobalInitOrder...),
		Globals:         make(map[ast.Ident]*PreparedGlobal, len(plan.Globals)),
		Functions:       make(map[ast.Ident]*PreparedFunction, len(plan.Functions)),
		MainTasks:       cloneTasks(plan.MainTasks),
	}

	for name, global := range plan.Globals {
		if global == nil {
			cloned.Globals[name] = nil
			continue
		}
		cloned.Globals[name] = &PreparedGlobal{
			Name:     global.Name,
			HasInit:  global.HasInit,
			InitPlan: cloneTasks(global.InitPlan),
		}
	}
	for name, fn := range plan.Functions {
		if fn == nil {
			cloned.Functions[name] = nil
			continue
		}
		cloned.Functions[name] = &PreparedFunction{
			Name:        fn.Name,
			FunctionSig: cloneRuntimeFuncSig(fn.FunctionSig),
			BodyTasks:   cloneTasks(fn.BodyTasks),
		}
	}

	return cloned
}
