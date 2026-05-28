package engine

import (
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/compiler"
)

type AnalysisProgram struct {
	Source   string
	Program  *ast.ProgramStmt
	Artifact *AnalysisArtifact

	// TemplatePreviews contains source-based call template render previews used by LSP hover.
	TemplatePreviews []calltemplate.TemplatePreview

	parentMap map[ast.Node]ast.Node
	parentMu  sync.RWMutex
}

func newAnalysisProgram(source string, compiled *compiler.Artifact, program *ast.ProgramStmt) *AnalysisProgram {
	artifact := analysisArtifactFromCompiled(source, compiled, program)
	res := &AnalysisProgram{
		Artifact: artifact,
	}
	if artifact != nil {
		res.Source = artifact.Source
		res.Program = artifact.Program
		res.TemplatePreviews = append([]calltemplate.TemplatePreview(nil), artifact.TemplatePreviews...)
	}
	return res
}

// ReleaseLSPCache releases analysis caches after IDE queries no longer need them.
func (p *AnalysisProgram) ReleaseLSPCache() {
	if p == nil {
		return
	}
	p.parentMu.Lock()
	defer p.parentMu.Unlock()
	p.parentMap = nil
}

func (p *AnalysisProgram) GetParent(node ast.Node) ast.Node {
	if p == nil || p.Program == nil || node == nil {
		return nil
	}
	p.parentMu.RLock()
	if p.parentMap != nil {
		if parent, ok := p.parentMap[node]; ok {
			p.parentMu.RUnlock()
			return parent
		}
	}
	p.parentMu.RUnlock()

	p.parentMu.Lock()
	defer p.parentMu.Unlock()
	if p.parentMap == nil {
		p.parentMap = ast.BuildParentMap(p.Program)
	}
	return p.parentMap[node]
}

func (p *AnalysisProgram) BuildAllCache() {
	if p == nil || p.Program == nil {
		return
	}
	p.parentMu.Lock()
	defer p.parentMu.Unlock()
	if p.parentMap == nil {
		p.parentMap = ast.BuildParentMap(p.Program)
	}
}

func (p *AnalysisProgram) GetNodeAt(line, col int) ast.Node {
	return p.GetNodeAtFile("", line, col)
}

func (p *AnalysisProgram) GetNodeAtFile(file string, line, col int) ast.Node {
	if p == nil || p.Program == nil {
		return nil
	}
	return ast.FindNodeAtFile(p.Program, file, line, col)
}

func (p *AnalysisProgram) GetDefinitionAt(line, col int) ast.Node {
	return p.GetDefinitionAtFile("", line, col)
}

func (p *AnalysisProgram) GetDefinitionAtFile(file string, line, col int) ast.Node {
	node := p.GetNodeAtFile(file, line, col)
	if node == nil {
		return nil
	}
	p.BuildAllCache()
	return ast.FindDefinition(p.Program, node, p.parentMap)
}

func (p *AnalysisProgram) GetHoverAt(line, col int) *ast.HoverInfo {
	return p.GetHoverAtFile("", line, col)
}

func (p *AnalysisProgram) GetHoverAtFile(file string, line, col int) *ast.HoverInfo {
	if p == nil {
		return nil
	}
	for _, preview := range p.TemplatePreviews {
		if preview.Contains(file, line, col) {
			return &ast.HoverInfo{Markdown: preview.Markdown()}
		}
	}
	node := p.GetNodeAtFile(file, line, col)
	if node == nil {
		return nil
	}
	p.BuildAllCache()
	return ast.FindHoverInfo(p.Program, node, p.parentMap)
}

func (p *AnalysisProgram) GetReferencesAt(line, col int, includeDeclaration bool) []ast.Node {
	return p.GetReferencesAtFile("", line, col, includeDeclaration)
}

func (p *AnalysisProgram) GetReferencesAtFile(file string, line, col int, includeDeclaration bool) []ast.Node {
	node := p.GetNodeAtFile(file, line, col)
	if node == nil {
		return nil
	}
	p.BuildAllCache()
	def := ast.FindDefinition(p.Program, node, p.parentMap)
	if def == nil {
		return nil
	}
	return ast.FindAllReferences(p.Program, def, p.parentMap, includeDeclaration)
}

func (p *AnalysisProgram) GetCompletionsAt(line, col int) []ast.CompletionItem {
	return p.GetCompletionsAtFile("", line, col)
}

func (p *AnalysisProgram) GetCompletionsAtFile(file string, line, col int) []ast.CompletionItem {
	if p == nil || p.Program == nil {
		return nil
	}
	return ast.FindCompletionsAtFile(p.Program, file, line, col)
}
