package ast

import (
	"errors"
	"fmt"
)

// IncDecStmt : i++ i--
type IncDecStmt struct {
	BaseNode
	Operand  Expr  `json:"operand"`
	Operator Ident `json:"operator"`
}

func (i *IncDecStmt) GetBase() *BaseNode { return &i.BaseNode }
func (i *IncDecStmt) stmtNode()          {}

func (i *IncDecStmt) Check(ctx *SemanticContext) error {
	if i.Operator != "++" && i.Operator != "--" {
		return fmt.Errorf("自增/自减操作符必须是 ++ 或 --, 实际为 %s", i.Operator)
	}
	_, ok := i.Operand.(*IdentifierExpr)
	if !ok {
		return errors.New("自增/自减操作的操作数必须是变量")
	}
	if err := i.Operand.Check(ctx); err != nil {
		return err
	}

	miniType := i.Operand.GetBase().Type
	if miniType.IsEmpty() {
		return errors.New("无法推导操作数类型")
	}
	i.Type = "Void"
	return nil
}

func (i *IncDecStmt) Optimize(ctx *OptimizeContext) Node {
	i.Operand = i.Operand.Optimize(ctx).(Expr)
	ident := i.Operand.(*IdentifierExpr)

	op := i.Operator
	if op == "++" {
		op = "Plus"
	} else {
		op = "Minus"
	}

	ret := &AssignmentStmt{
		BaseNode: BaseNode{
			ID:      i.ID,
			Meta:    "assignment",
			Type:    "Void",
			Message: i.Message,
		},
		Variable: ident.Name,
		Value: &BinaryExpr{
			BaseNode: BaseNode{
				ID:      i.ID + "_Value_0",
				Meta:    "binary",
				Message: i.Message,
			},

			Left:     i.Operand,
			Operator: op,
			Right: &LiteralExpr{
				BaseNode: BaseNode{
					ID:   i.ID + "_Value_0_Right_0",
					Meta: "literal",
					Type: "Int64",
				},
				Value: "1",
			},
		},
	}
	// AssignmentStmt.Optimize will handle further optimization
	return ret.Optimize(ctx)
}

// SwitchCase 表示switch语句中的case分支
type SwitchCase struct {
	Cond []Expr     `json:"cond"`
	Body *BlockStmt `json:"body"`
}

// SwitchStmt 表示switch多分支选择语句
type SwitchStmt struct {
	BaseNode
	Cond    Expr         `json:"cond"`
	Cases   []SwitchCase `json:"cases"`
	Default *SwitchCase  `json:"default,omitempty"`
}

func (s *SwitchStmt) GetBase() *BaseNode { return &s.BaseNode }
func (s *SwitchStmt) stmtNode()          {}

func (s *SwitchStmt) Check(ctx *SemanticContext) error {
	if s.Cond == nil {
		return errors.New("switch语句缺少条件表达式")
	}

	if err := s.Cond.Check(ctx); err != nil {
		return err
	}

	condType := s.Cond.GetBase().Type
	if condType.IsEmpty() {
		return errors.New("switch条件表达式类型无法推导")
	}

	if len(s.Cases) == 0 && s.Default == nil {
		return errors.New("switch语句至少需要一个case或default分支")
	}

	switchCtx := ctx.Child(s)
	semSwitchCtx := NewSemanticContext(switchCtx)

	// 验证所有case和default
	for i, caseItem := range s.Cases {
		if len(caseItem.Cond) == 0 {
			return fmt.Errorf("switch case %d 缺少条件表达式", i)
		}

		for _, cond := range caseItem.Cond {
			if err := cond.Check(semSwitchCtx); err != nil {
				return err
			}

			caseType := cond.GetBase().Type
			if caseType != "" && !condType.Equals(caseType) {
				return fmt.Errorf("case条件类型不匹配: switch条件为 %s, case条件为 %s", condType, caseType)
			}
		}

		if caseItem.Body == nil {
			return fmt.Errorf("switch case %d 缺少主体", i)
		}

		if err := caseItem.Body.Check(semSwitchCtx); err != nil {
			return err
		}
	}

	if s.Default != nil {
		if s.Default.Body == nil {
			return errors.New("switch default分支缺少主体")
		}

		if err := s.Default.Body.Check(semSwitchCtx); err != nil {
			return err
		}
	}
	s.Type = "Void"
	return nil
}

func (s *SwitchStmt) Optimize(ctx *OptimizeContext) Node {
	s.Cond = s.Cond.Optimize(ctx).(Expr)
	for i, caseItem := range s.Cases {
		for j, cond := range caseItem.Cond {
			s.Cases[i].Cond[j] = cond.Optimize(ctx).(Expr)
		}
		s.Cases[i].Body = caseItem.Body.Optimize(ctx).(*BlockStmt)
	}
	if s.Default != nil {
		s.Default.Body = s.Default.Body.Optimize(ctx).(*BlockStmt)
	}

	ifElse := s.convertToIfElse()
	return ifElse.Optimize(ctx)
}

// convertToIfElse 将switch语句转换为if-else链
func (s *SwitchStmt) convertToIfElse() Node {
	if len(s.Cases) == 0 && s.Default == nil {
		return NewBlock(s)
	}
	var result *BlockStmt

	// 如果有default分支，将其作为最后的else
	if s.Default != nil && s.Default.Body != nil {
		result = s.Default.Body
	} else {
		// 没有default，创建一个空的else分支
		result = NewBlock(s)
	}

	// 从最后一个case向前构建
	for i := len(s.Cases) - 1; i >= 0; i-- {
		caseItem := s.Cases[i]
		if len(caseItem.Cond) == 0 || caseItem.Body == nil {
			continue
		}
		// 构建case的条件表达式：多个条件用"or"连接
		var cond Expr
		caseID := fmt.Sprintf("%s_Cases_%d", s.ID, i)
		if len(caseItem.Cond) == 1 {
			// 单个条件：cond == caseCond
			cond = &BinaryExpr{
				BaseNode: BaseNode{
					ID:      caseID + "_Cond_0",
					Meta:    "binary",
					Type:    "Bool",
					Message: s.Message,
				},
				Left:     s.Cond,
				Operator: "Eq",
				Right:    caseItem.Cond[0],
			}
		} else {
			// 多个条件：(cond == caseCond1) or (cond == caseCond2) or ...
			var orExpr []Expr
			for j, caseCond := range caseItem.Cond {
				eqExpr := &BinaryExpr{
					BaseNode: BaseNode{
						ID:      fmt.Sprintf("%s_Cond_%d", caseID, j),
						Meta:    "binary",
						Type:    "Bool",
						Message: s.Message,
					},
					Left:     s.Cond,
					Operator: "Eq",
					Right:    caseCond,
				}
				orExpr = append(orExpr, eqExpr)
			}

			// 递归构建or表达式链
			cond = buildOrChain(orExpr, caseID, s.Message)
		}

		// 构建if语句
		ifStmt := &IfStmt{
			BaseNode: BaseNode{
				ID:      caseID,
				Meta:    "if",
				Type:    "Void",
				Message: s.Message,
			},
			Cond:     cond,
			Body:     caseItem.Body,
			ElseBody: result,
		}
		result = NewBlock(ifStmt, ifStmt)
		if i == 0 {
			result.ID = s.ID
		}
		ifStmt.GetBase().ID = result.ID + "_Children_0"
	}
	return result
}

// buildOrChain 递归构建or表达式链
func buildOrChain(expr []Expr, baseID, message string) Expr {
	if len(expr) == 0 {
		return nil
	}
	if len(expr) == 1 {
		return expr[0]
	}

	// 递归构建：expr1 or (expr2 or (expr3 ...))
	right := buildOrChain(expr[1:], baseID, message)
	return &BinaryExpr{
		BaseNode: BaseNode{
			ID:      fmt.Sprintf("%s_Or_%d", baseID, len(expr)),
			Meta:    "binary",
			Type:    "Bool",
			Message: message,
		},
		Left:     expr[0],
		Operator: "Or",
		Right:    right,
	}
}
