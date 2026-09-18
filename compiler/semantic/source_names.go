package semantic

import "github.com/d7z-team/mini-go/compiler/ast"

// BindSourceNames projects exact identifier definitions and uses onto an
// already checked program. It is tooling data and is built only on demand.
func BindSourceNames(program ast.Program, info *ProgramInfo) {
	if info == nil {
		return
	}
	info.NameDefs = make(map[ast.NameID]ObjectID)
	info.NameUses = make(map[ast.NameID]ObjectID)
	usedDefinitions := map[ObjectID]bool{}
	for _, occurrence := range ast.Names(program) {
		switch occurrence.Role {
		case ast.NameDefinition, ast.NameImport:
			for _, objectID := range info.Defs[occurrence.Node] {
				object := info.Objects[objectID]
				if !usedDefinitions[objectID] && object.Name == occurrence.Name.Text {
					info.NameDefs[occurrence.Name.ID] = objectID
					object.Definition = occurrence.Name
					info.Objects[objectID] = object
					usedDefinitions[objectID] = true
					break
				}
			}
		case ast.NameReference:
			if objectID := info.Uses[occurrence.Node]; objectID != "" {
				info.NameUses[occurrence.Name.ID] = objectID
			}
		case ast.NameSelector:
			if selection, ok := info.Selections[occurrence.Node]; ok && selection.Object != "" {
				info.NameUses[occurrence.Name.ID] = selection.Object
			}
		}
	}
}
