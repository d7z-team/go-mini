package ast

import "fmt"

// ReturnAnalyzer 返回分析器，用于检查函数是否所有路径都有返回值
type ReturnAnalyzer struct {
	ctx         *ValidContext
	returnTypes []GoMiniType
	errors      []AnalysisError
}

// AnalysisError 分析错误
type AnalysisError struct {
	Path    []string
	Message string
}

// NewReturnAnalyzer 创建新的返回分析器
func NewReturnAnalyzer(ctx *ValidContext, returnType GoMiniType) *ReturnAnalyzer {
	returnTypes, _ := returnType.ReadTuple()

	return &ReturnAnalyzer{
		ctx:         ctx,
		returnTypes: returnTypes,
		errors:      make([]AnalysisError, 0),
	}
}

// Analyze 分析函数体，返回是否所有路径都有返回值
func (a *ReturnAnalyzer) Analyze(body Node) bool {
	if len(a.returnTypes) == 0 || (len(a.returnTypes) == 1 && a.returnTypes[0].IsVoid()) {
		return true // void函数不需要检查
	}

	// 分析函数体
	hasReturn := a.analyzeNode(body)

	if !hasReturn {
		a.addError("函数缺少返回语句或并非所有分支都有返回语句", body)
		return false
	}

	return len(a.errors) == 0
}

// analyzeNode 分析节点，返回当前节点是否所有路径都有返回值
func (a *ReturnAnalyzer) analyzeNode(node Node) bool {
	if node == nil {
		return false
	}

	switch n := node.(type) {
	case *BlockStmt:
		return a.analyzeBlock(n)
	case *IfStmt:
		return a.analyzeIf(n)
	case *ForStmt:
		return a.analyzeFor(n)
	case *ReturnStmt:
		return a.analyzeReturn(n)
	case *InterruptStmt:
		return a.analyzeInterrupt(n)
	default:
		// 其他节点不会产生返回
		return false
	}
}

// analyzeBlock 分析代码块
func (a *ReturnAnalyzer) analyzeBlock(block *BlockStmt) bool {
	if block == nil || len(block.Children) == 0 {
		return false
	}
	for _, child := range block.Children {
		if a.analyzeNode(child) {
			// 如果当前语句有返回，整个块就返回
			// 注意：这里的实现假设一旦有返回语句，后续代码不会执行
			// 这是保守假设，符合大多数编程语言语义
			return true
		}
	}

	return false // 块中没有返回语句
}

func (a *ReturnAnalyzer) analyzeIf(ifStmt *IfStmt) bool {
	if ifStmt == nil {
		return false
	}
	thenReturns := a.analyzeNode(ifStmt.Body)

	elseReturns := false
	if ifStmt.ElseBody != nil {
		elseReturns = a.analyzeNode(ifStmt.ElseBody)
	}
	// 如果没有else分支，if语句可能不会返回
	if ifStmt.ElseBody == nil {
		// 没有else分支，无法保证所有路径都有返回
		return false
	}
	// 两个分支都有返回，则if语句返回
	return thenReturns && elseReturns
}

// analyzeFor 分析for循环
func (a *ReturnAnalyzer) analyzeFor(forStmt *ForStmt) bool {
	if forStmt == nil {
		return false
	}

	bodyReturns := a.analyzeNode(forStmt.Body)

	// 如果是无限循环 (没有条件) 且内部保证了返回 (且不考虑复杂的 break 逃逸，或者假设验证器能识别 break)
	// 在严格安全的引擎中，为了避免死循环报错被屏蔽，通常要求无限循环如果有返回则视为必返回。
	// 由于我们没有跨分支跟踪 break，这里做保守优化：只有当明确无条件且 body 返回时才视为真
	if forStmt.Cond == nil && bodyReturns {
		return true
	}

	return false
}

// analyzeReturn 分析return语句
func (a *ReturnAnalyzer) analyzeReturn(returnStmt *ReturnStmt) bool {
	if returnStmt == nil {
		return false
	}
	// 检查返回类型是否匹配
	if len(a.returnTypes) > 0 {
		var actualTypes []GoMiniType
		if len(returnStmt.Results) == 1 {
			actualTypes = []GoMiniType{returnStmt.Results[0].GetBase().Type}
		} else if len(returnStmt.Results) > 1 {
			for _, result := range returnStmt.Results {
				actualTypes = append(actualTypes, result.GetBase().Type)
			}
		}
		if !a.compareReturnTypes(actualTypes, a.returnTypes) {
			a.addError(fmt.Sprintf("返回类型不匹配: 期望 %v, 实际 %v", a.returnTypes, actualTypes), returnStmt)
		}
	}
	return true
}

// analyzeInterrupt 分析中断语句
func (a *ReturnAnalyzer) analyzeInterrupt(interrupt *InterruptStmt) bool {
	if interrupt == nil {
		return false
	}

	// 中断语句不会产生返回值
	return false
}

// compareReturnTypes 比较返回类型
func (a *ReturnAnalyzer) compareReturnTypes(actual, expected []GoMiniType) bool {
	if len(actual) != len(expected) {
		return false
	}

	for i := range actual {
		if !actual[i].Equals(expected[i]) {
			return false
		}
	}

	return true
}

// addError 添加错误
func (a *ReturnAnalyzer) addError(message string, node Node) {
	path := a.buildPath(node)
	a.errors = append(a.errors, AnalysisError{
		Path:    path,
		Message: message,
	})
}

// buildPath 构建节点路径
func (a *ReturnAnalyzer) buildPath(node Node) []string {
	path := make([]string, 0)

	if node != nil {
		base := node.GetBase()
		typeName := getTypeName(node)
		path = append(path, fmt.Sprintf("%s#%s", typeName, base.ID))
	}

	return path
}

// getTypeName 获取类型名
func getTypeName(v interface{}) string {
	typeStr := fmt.Sprintf("%T", v)

	// 提取类型名，去掉包名
	for i := len(typeStr) - 1; i >= 0; i-- {
		if typeStr[i] == '.' {
			return typeStr[i+1:]
		}
	}

	return typeStr
}

// GetErrors 获取所有错误
func (a *ReturnAnalyzer) GetErrors() []AnalysisError {
	return a.errors
}

// AddReturnPathErrorsToContext 将返回路径错误添加到验证上下文
func (a *ReturnAnalyzer) AddReturnPathErrorsToContext(v *ValidContext) {
	for _, err := range a.errors {
		v.AddErrorf("%s", err.Message)
	}
}
