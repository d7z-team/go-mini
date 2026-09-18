package specialize

import (
	"strconv"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/cache"
	check "github.com/d7z-team/mini-go/compiler/semantic"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/types"
)

// bindDotImports makes imported references explicit before declarations are
// copied for specialization. The semantic binding, not the spelling, identifies
// each reference and preserves local shadowing and definition-site scope.
func (s *genericSpecializer) bindDotImports(program *ast.Program) {
	aliases := make(map[string]string)
	used := make(map[string]bool, len(s.info.Objects))
	for _, object := range s.info.Objects {
		used[object.Name] = true
	}
	for i := range program.Files {
		for j := range program.Files[i].Decls {
			decl := &program.Files[i].Decls[j]
			if decl.Kind != ast.DeclImport || decl.Import.Alias != "." {
				continue
			}
			path := decl.Import.Path
			alias := aliases[path]
			if alias == "" {
				for n := len(aliases); ; n++ {
					alias = "_generic_import_" + strconv.Itoa(n)
					if !used[alias] {
						break
					}
				}
				used[alias] = true
				aliases[path] = alias
			}
			decl.Import.Alias = alias
		}
	}
	if len(aliases) == 0 {
		return
	}
	ast.WalkExpressions(program, func(expr *ast.Expression) {
		if expr.Kind != ast.ExprIdent {
			return
		}
		object, ok := s.info.Object(s.info.Uses[expr.NodeID])
		if !ok || object.ExportName == "" || aliases[object.ModulePath] == "" {
			return
		}
		operand := ast.Expression{Kind: ast.ExprIdent, Name: aliases[object.ModulePath], Span: expr.Span}
		*expr = ast.Expression{NodeID: expr.NodeID, Kind: ast.ExprSelector, Operand: &operand, Field: object.ExportName, Span: expr.Span}
	})
	ast.WalkTypes(program, func(typ *ast.TypeExpr) {
		if typ.Kind != ast.TypeName {
			return
		}
		object, ok := s.info.Object(s.info.Uses[typ.NodeID])
		if ok && object.Kind == check.ObjectType && object.ExportName != "" && aliases[object.ModulePath] != "" {
			typ.Name = aliases[object.ModulePath] + "." + object.ExportName
		}
	})
}

func (s *genericSpecializer) collectImportedGenerics(program ast.Program, dependencies map[string]cache.PackageData) {
	collected := make(map[string]bool)
	collect := func(alias string, data cache.PackageData, file string) {
		collected[data.ModulePath] = true
		if alias == "" {
			alias = strings.TrimSpace(data.Package)
		}
		s.importPaths[alias] = data.ModulePath
		namedTypes := importedNamedTypes(data)
		for name, underlying := range namedTypes {
			s.typeDefs[alias+"."+name] = underlying
		}
		for _, typeDecl := range data.TypeTable.DefinedNamed(data.ModulePath) {
			receiver := alias + "." + string(typeDecl.Identity.DeclID)
			for _, method := range typeDecl.Methods {
				decl := ast.FuncDecl{Name: method.Name}
				for _, param := range method.Signature.Params {
					decl.Params = append(decl.Params, ast.Field{Type: importedTypeExpr(&data.TypeTable, param.Type)})
				}
				for _, result := range method.Signature.Results {
					decl.Results = append(decl.Results, ast.Field{Type: importedTypeExpr(&data.TypeTable, result)})
				}
				if method.Signature.Variadic && len(decl.Params) != 0 {
					decl.Params[len(decl.Params)-1].Variadic = true
				}
				s.registerMethod(receiver, decl, types.View(&data.TypeTable, method.Receiver).Shape() == types.Pointer)
			}
		}
		templateFiles := make(map[string]struct{}, len(data.GenericTemplates))
		for _, template := range data.GenericTemplates {
			decl := template.Decl
			references := make(map[ast.NodeID]string, len(template.References))
			referenceOffsets := make(map[int]string, len(template.References))
			for _, reference := range template.References {
				references[reference.Node] = strings.TrimSpace(reference.Name)
				referenceOffsets[reference.Span.Start.Offset] = strings.TrimSpace(reference.Name)
			}
			if path := strings.TrimSpace(decl.Span.Start.File); path != "" {
				templateFiles[path] = struct{}{}
			}
			ast.RewriteDeclSourcePaths(&decl, func(path string) string {
				return importedSourcePath(data.ModulePath, path)
			})
			generic := genericDecl{
				decl: decl, file: file, importAlias: alias, namedTypes: namedTypes,
				references: references, referenceOffsets: referenceOffsets,
			}
			switch template.Kind {
			case "function":
				generic.typeParams = decl.Func.TypeParams
				s.functions[alias+"."+template.Name] = generic
			case "type":
				generic.typeParams = decl.Type.TypeParams
				s.types[alias+"."+template.Name] = generic
			case "method":
				parts := strings.SplitN(template.Name, ".", 2)
				if len(parts) != 2 {
					continue
				}
				owner := alias + "." + parts[0]
				s.methods[owner] = append(s.methods[owner], generic)
			}
		}
		for _, source := range data.SourceFiles {
			if _, ok := templateFiles[strings.TrimSpace(source.ID)]; !ok {
				if _, ok = templateFiles[strings.TrimSpace(source.Path)]; !ok {
					continue
				}
			}
			path := importedSourcePath(data.ModulePath, source.Path)
			s.importedFiles[path] = ast.File{ID: path, Path: path, Hash: source.Hash}
		}
	}

	for i := range program.Files {
		for j := range program.Files[i].Decls {
			decl := program.Files[i].Decls[j]
			if decl.Kind != ast.DeclImport || decl.Import.Alias == "_" {
				continue
			}
			importPath := strings.TrimSpace(decl.Import.Path)
			data, ok := dependencies[importPath]
			if !ok {
				continue
			}
			collect(strings.TrimSpace(decl.Import.Alias), data, program.Files[i].Path)
		}
	}
	for modulePath, data := range dependencies {
		if !collected[modulePath] {
			collect(strings.TrimSpace(data.Package), data, "")
		}
	}
}

func importedNamedTypes(data cache.PackageData) map[string]ast.TypeExpr {
	declarations := data.TypeTable.DefinedNamed(data.ModulePath)
	out := make(map[string]ast.TypeExpr, len(declarations))
	for _, decl := range declarations {
		out[string(decl.Identity.DeclID)] = importedTypeExpr(&data.TypeTable, decl.Underlying)
	}
	return out
}

func importedTypeExpr(table *types.TypeTable, ref types.TypeRef) ast.TypeExpr {
	if ref.Kind == types.Named {
		return ast.TypeExpr{Kind: ast.TypeName, Name: types.FormatWithTable(table, ref)}
	}
	view := types.View(table, ref)
	switch view.Shape() {
	case types.Slice:
		elemRef, _ := view.Elem()
		elem := importedTypeExpr(table, elemRef)
		return ast.TypeExpr{Kind: ast.TypeSlice, Elem: &elem}
	case types.Pointer:
		elemRef, _ := view.Elem()
		elem := importedTypeExpr(table, elemRef)
		return ast.TypeExpr{Kind: ast.TypePointer, Elem: &elem}
	case types.Map:
		keyRef, elemRef, _ := view.Map()
		key := importedTypeExpr(table, keyRef)
		elem := importedTypeExpr(table, elemRef)
		return ast.TypeExpr{Kind: ast.TypeMap, Key: &key, Elem: &elem}
	case types.Interface:
		node, ok := table.Node(view.Underlying())
		if !ok {
			return ast.TypeExpr{Kind: ast.TypeName, Name: types.FormatWithTable(table, ref)}
		}
		result := ast.TypeExpr{Kind: ast.TypeInterface}
		for _, term := range node.Terms {
			result.Terms = append(result.Terms, ast.TypeTerm{Type: importedTypeExpr(table, term.Type), Approx: term.Approx})
		}
		for _, method := range node.Methods {
			fn := ast.FuncDecl{Name: method.Name}
			for _, param := range method.Signature.Params {
				fn.Params = append(fn.Params, ast.Field{Type: importedTypeExpr(table, param.Type)})
			}
			for _, resultType := range method.Signature.Results {
				fn.Results = append(fn.Results, ast.Field{Type: importedTypeExpr(table, resultType)})
			}
			result.Methods = append(result.Methods, fn)
		}
		return result
	}
	text := types.FormatWithTable(table, ref)
	return ast.TypeExpr{Kind: ast.TypeName, Name: text}
}

func (s *genericSpecializer) retainImportInitializers(program *ast.Program, original ast.Program) {
	sourceFiles := make(map[ast.NodeID]string, len(original.Files))
	for _, file := range original.Files {
		sourceFiles[file.NodeID] = file.Span.Start.File
	}
	for i := range program.Files {
		file := &program.Files[i]
		used := make(map[string]bool)
		mark := func(node ast.NodeID, span source.Span, name string) {
			objectID := s.info.Uses[node]
			if objectID == "" && len(s.info.Defs[node]) != 0 {
				objectID = s.info.Defs[node][0]
			}
			object, known := s.info.Object(objectID)
			scope := s.info.Scope(s.info.NodeScopes[node])
			for scope != nil && scope.Kind != check.ScopeFile {
				scope = s.info.Scope(scope.Parent)
			}
			// Original bindings distinguish package references from shadowed
			// locals. Imported templates have different source paths and IDs.
			if known && object.Name == name && scope != nil && sourceFiles[scope.Node] == span.Start.File {
				binding := s.info.Scope(object.Scope)
				if binding != nil && binding.Kind != check.ScopeFile {
					return
				}
			}
			used[name] = true
		}
		fileProgram := ast.Program{Files: []ast.File{*file}}
		ast.WalkExpressions(&fileProgram, func(expr *ast.Expression) {
			if expr.Kind == ast.ExprSelector && expr.Operand != nil && expr.Operand.Kind == ast.ExprIdent {
				mark(expr.Operand.NodeID, expr.Operand.Span, expr.Operand.Name)
			} else if expr.Kind == ast.ExprIdent && s.info.Uses[expr.NodeID] != "" {
				mark(expr.NodeID, expr.Span, expr.Name)
			}
		})
		ast.WalkTypes(&fileProgram, func(typ *ast.TypeExpr) {
			if typ.Kind == ast.TypeName {
				name := typ.Name
				if dot := strings.IndexByte(name, '.'); dot >= 0 {
					name = name[:dot]
				}
				mark(typ.NodeID, typ.Span, name)
			}
		})
		for j := range file.Decls {
			decl := &file.Decls[j]
			if decl.Kind != ast.DeclImport || decl.Import.IsEmbedMarker() {
				continue
			}
			alias := decl.Import.Alias
			if alias == "_" {
				continue
			}
			if alias == "" {
				alias = decl.Import.Path
				if slash := strings.LastIndexByte(alias, '/'); slash >= 0 {
					alias = alias[slash+1:]
				}
			}
			// Source imports were checked before rewriting. Keep initialization
			// when all references in this file became local specializations.
			if !used[alias] {
				decl.Import.Alias = "_"
			}
		}
	}
}

func importedSourcePath(modulePath, path string) string {
	modulePath = strings.Trim(strings.TrimSpace(modulePath), "/")
	path = strings.TrimLeft(strings.TrimSpace(path), "/")
	if modulePath == "" || strings.HasPrefix(path, "<") {
		return path
	}
	if path == modulePath || strings.HasPrefix(path, modulePath+"/") {
		return path
	}
	return modulePath + "/" + path
}
