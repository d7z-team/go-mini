package gofrontend

import (
	"fmt"
	"go/token"
	"strconv"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

func convertBasicLiteralParts(kind token.Token, raw string) (miniast.GoMiniType, string, error) {
	switch kind {
	case token.INT:
		value, err := strconv.ParseInt(raw, 0, 64)
		if err != nil {
			return "", "", fmt.Errorf("invalid Int64 literal %s: %w", raw, err)
		}
		return miniast.TypeInt64, strconv.FormatInt(value, 10), nil
	case token.FLOAT:
		return miniast.TypeFloat64, raw, nil
	case token.STRING:
		if unquoted, err := strconv.Unquote(raw); err == nil {
			return miniast.TypeString, unquoted, nil
		}
		if len(raw) >= 2 {
			return miniast.TypeString, raw[1 : len(raw)-1], nil
		}
		return miniast.TypeString, raw, nil
	case token.CHAR:
		if len(raw) < 2 || raw[0] != '\'' || raw[len(raw)-1] != '\'' {
			return "", "", fmt.Errorf("invalid rune literal %s", raw)
		}
		value, _, tail, err := strconv.UnquoteChar(raw[1:len(raw)-1], '\'')
		if err != nil || tail != "" {
			return "", "", fmt.Errorf("invalid rune literal %s", raw)
		}
		return miniast.TypeRune, strconv.FormatInt(int64(value), 10), nil
	default:
		return "", "", fmt.Errorf("unsupported literal %s", kind)
	}
}
