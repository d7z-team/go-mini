package lower

import "github.com/d7z-team/mini-go/compiler/types"

func hirTypeString(table *types.TypeTable, ref types.TypeRef) string {
	return types.FormatWithTable(table, ref)
}
