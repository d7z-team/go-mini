// Package parser parses Mini-Go source and owns its immutable lossless document.
package parser

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/scanner"
	"github.com/d7z-team/mini-go/compiler/source"
)

type Document struct {
	File        source.File
	Elements    []scanner.Element
	Tokens      []scanner.Token
	Program     ast.Program
	Diagnostics []source.Diagnostic
	NodeCount   int
}

func ParseDocument(modulePath, path, text string) Document {
	return ParseDocumentFile(modulePath, source.NewFile("file.0", path, text))
}

func ParseDocumentFile(modulePath string, file source.File) Document {
	return ParseDocumentFileWithLimits(modulePath, file, Limits{})
}

func ParseDocumentFileWithLimits(modulePath string, file source.File, limits Limits) Document {
	limits = normalizeLimits(limits)
	scanned := scanner.ScanFileWithLimits(file, limits.Scanner)
	parsed := parseScanned(modulePath, scanned, limits)
	return Document{
		File:        scanned.File,
		Elements:    append([]scanner.Element(nil), scanned.Elements...),
		Tokens:      append([]scanner.Token(nil), scanned.Tokens...),
		Program:     parsed.Program,
		Diagnostics: append([]source.Diagnostic(nil), parsed.Diagnostics...),
		NodeCount:   parsed.NodeCount,
	}
}
