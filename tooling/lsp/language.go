// Package lsp adapts the native compiler language service to LSP.
package lsp

import (
	"context"

	doccore "github.com/d7z-team/mini-go/compiler/doc"
	"github.com/d7z-team/mini-go/compiler/language"
	"github.com/d7z-team/mini-go/tooling/doc"
	"github.com/d7z-team/mini-go/tooling/format"
)

func RangesOverlap(left, right Range) bool { return language.RangesOverlap(left, right) }

type (
	CodeAction                      = language.CodeAction
	CompletionItem                  = language.CompletionItem
	CompletionList                  = language.CompletionList
	Config                          = language.Config
	ContentChange                   = language.ContentChange
	Diagnostic                      = language.Diagnostic
	DiagnosticRelatedInformation    = language.DiagnosticRelatedInformation
	DiagnosticReport                = language.DiagnosticReport
	Document                        = language.Document
	DocumentHighlight               = language.DocumentHighlight
	DocumentIdentity                = language.DocumentIdentity
	DocumentStore                   = language.DocumentStore
	DocumentSymbol                  = language.DocumentSymbol
	DocumentURI                     = language.DocumentURI
	Engine                          = language.Engine
	FoldingRange                    = language.FoldingRange
	Hover                           = language.Hover
	Limits                          = language.Limits
	LineIndex                       = language.LineIndex
	Location                        = language.Location
	MarkupContent                   = language.MarkupContent
	ParameterInformation            = language.ParameterInformation
	Position                        = language.Position
	Range                           = language.Range
	SelectionRange                  = language.SelectionRange
	SemanticTokens                  = language.SemanticTokens
	SignatureHelp                   = language.SignatureHelp
	SignatureInformation            = language.SignatureInformation
	Snapshot                        = language.Snapshot
	SymbolInformation               = language.SymbolInformation
	TextDocumentEdit                = language.TextDocumentEdit
	TextDocumentIdentifier          = language.TextDocumentIdentifier
	TextEdit                        = language.TextEdit
	VersionedTextDocumentIdentifier = language.VersionedTextDocumentIdentifier
	WorkspaceEdit                   = language.WorkspaceEdit
)

var (
	ErrDocumentNotFound    = language.ErrDocumentNotFound
	ErrVersionRollback     = language.ErrVersionRollback
	ErrFileTooLarge        = language.ErrFileTooLarge
	ErrInvalidPosition     = language.ErrInvalidPosition
	SemanticTokenTypes     = language.SemanticTokenTypes
	SemanticTokenModifiers = language.SemanticTokenModifiers
)

func DefaultLimits() Limits                         { return language.DefaultLimits() }
func NewLineIndex(text string) LineIndex            { return language.NewLineIndex(text) }
func NewDocumentStore(limits Limits) *DocumentStore { return language.NewDocumentStore(limits) }
func NewEngine(config Config) (*Engine, error)      { return NewEngineContext(context.Background(), config) }

func NewEngineContext(ctx context.Context, config Config) (*Engine, error) {
	if config.RenderDocumentation == nil {
		config.RenderDocumentation = func(catalog doccore.Catalog) func(string, doccore.Comment) string {
			renderer := doc.NewRenderer(catalog)
			return renderer.RenderComment
		}
	}
	if config.FormatSource == nil {
		config.FormatSource = format.Source
	}
	return language.NewEngineContext(ctx, config)
}
