package ast

import "fmt"

// IncDecStmt : i++ i--
type IncDecStmt struct {
	BaseNode
	Operand  Expr  `json:"operand"`
	Operator Ident `json:"operator"`
}

func (i *IncDecStmt) GetBase() *BaseNode { return &i.BaseNode }
func (i *IncDecStmt) stmtNode()          {}

func (i *IncDecStmt) Validate(ctx *ValidContext) (Node, bool) {
	ctx = ctx.Child(i)
	if i.Operator != "++" && i.Operator != "--" {
		ctx.AddErrorf("自增/自减操作符必须是 ++ 或 --, 实际为 %s", i.Operator)
		return nil, false
	}
	ident, ok := i.Operand.(*IdentifierExpr)
	if !ok {
		ctx.Child(i.Operand).AddErrorf("自增/自减操作的操作数必须是变量")
		return nil, false
	}
	operandNode, ok := i.Operand.Validate(ctx)
	if !ok {
		return nil, false
	}
	i.Operand = operandNode.(Expr)

	miniType := i.Operand.GetBase().Type
	if miniType.IsEmpty() {
		ctx.Child(i.Operand).AddErrorf("无法推导操作数类型")
		return nil, false
	}
	if i.Operator == "++" {
		i.Operator = "Plus"
	} else {
		i.Operator = "Minus"
	}
	i.Type = "Void"
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
			Operator: i.Operator,
			Right: &LiteralExpr{
				BaseNode: BaseNode{
					ID:   i.ID + "_Value_0_Right_0",
					Meta: "literal",
					Type: "Number",
				},
				Value: "1",
			},
		},
	}
	node, b := ret.Validate(ctx)
	if !b {
		return nil, false
	}
	return node, true
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

func (s *SwitchStmt) Validate(ctx *ValidContext) (Node, bool) {
	ctx = ctx.Child(s)

	if s.Cond == nil {
		ctx.AddErrorf("switch语句缺少条件表达式")
		return nil, false
	}

	condNode, ok := s.Cond.Validate(ctx)
	if !ok {
		return nil, false
	}
	s.Cond = condNode.(Expr)

	condType := s.Cond.GetBase().Type
	if condType.IsEmpty() {
		ctx.Child(s.Cond).AddErrorf("switch条件表达式类型无法推导")
		return nil, false
	}

	if len(s.Cases) == 0 && s.Default == nil {
		ctx.AddErrorf("switch语句至少需要一个case或default分支")
		return nil, false
	}

	switchCtx := ctx.Child(s)

	// 验证所有case和default
	for i, caseItem := range s.Cases {
		if len(caseItem.Cond) == 0 {
			ctx.AddErrorf("switch case %d 缺少条件表达式", i)
			return nil, false
		}

		for j, cond := range caseItem.Cond {
			condNode, ok := cond.Validate(switchCtx)
			if !ok {
				return nil, false
			}
			s.Cases[i].Cond[j] = condNode.(Expr)

			caseType := cond.GetBase().Type
			if caseType != "" && !condType.Equals(caseType) {
				ctx.Child(cond).AddErrorf("case条件类型不匹配: switch条件为 %s, case条件为 %s", condType, caseType)
				return nil, false
			}
		}

		if caseItem.Body == nil {
			ctx.AddErrorf("switch case %d 缺少主体", i)
			return nil, false
		}

		bodyNode, ok := caseItem.Body.Validate(switchCtx)
		if !ok {
			return nil, false
		}
		s.Cases[i].Body = bodyNode.(*BlockStmt)
	}

	if s.Default != nil {
		if s.Default.Body == nil {
			ctx.AddErrorf("switch default分支缺少主体")
			return nil, false
		}

		bodyNode, ok := s.Default.Body.Validate(switchCtx)
		if !ok {
			return nil, false
		}
		s.Default.Body = bodyNode.(*BlockStmt)
	}
	s.Type = "Void"
	ifElse := s.convertToIfElse()
	return ifElse.Validate(ctx)
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
