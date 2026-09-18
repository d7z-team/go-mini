package language

import (
	"errors"
	"strings"
)

var (
	ErrDocumentNotFound = errors.New("document not found")
	ErrVersionRollback  = errors.New("document version must increase")
	ErrFileTooLarge     = errors.New("document exceeds file size limit")
)

type Document struct {
	Identity DocumentIdentity
	Version  int
	Text     string
	Index    LineIndex
	Open     bool
}

type DocumentStore struct {
	documents map[DocumentURI]Document
	limits    Limits
}

func NewDocumentStore(limits Limits) *DocumentStore {
	if limits.MaxFileBytes <= 0 {
		limits = DefaultLimits()
	}
	return &DocumentStore{documents: map[DocumentURI]Document{}, limits: limits}
}

func (s *DocumentStore) clone() *DocumentStore {
	documents := make(map[DocumentURI]Document, len(s.documents))
	for uri, document := range s.documents {
		documents[uri] = document
	}
	return &DocumentStore{documents: documents, limits: s.limits}
}

func (s *DocumentStore) Seed(identity DocumentIdentity, text string) error {
	if err := s.validate(identity, text); err != nil {
		return err
	}
	s.documents[identity.URI] = Document{Identity: identity, Version: 0, Text: text, Index: NewLineIndex(text)}
	return nil
}

func (s *DocumentStore) Open(identity DocumentIdentity, version int, text string) error {
	if err := s.validate(identity, text); err != nil {
		return err
	}
	s.documents[identity.URI] = Document{Identity: identity, Version: version, Text: text, Index: NewLineIndex(text), Open: true}
	return nil
}

func (s *DocumentStore) Change(uri DocumentURI, version int, changes []ContentChange) error {
	document, ok := s.documents[uri]
	if !ok {
		return ErrDocumentNotFound
	}
	if version <= document.Version {
		return ErrVersionRollback
	}
	text := document.Text
	index := document.Index
	for _, change := range changes {
		if change.Range == nil {
			text = change.Text
			index = NewLineIndex(text)
			continue
		}
		start, err := index.Offset(change.Range.Start)
		if err != nil {
			return err
		}
		end, err := index.Offset(change.Range.End)
		if err != nil || end < start {
			return ErrInvalidPosition
		}
		text = text[:start] + change.Text + text[end:]
		index = NewLineIndex(text)
	}
	if s.limits.MaxFileBytes > 0 && len(text) > s.limits.MaxFileBytes {
		return ErrFileTooLarge
	}
	document.Version, document.Text, document.Index, document.Open = version, text, index, true
	s.documents[uri] = document
	return nil
}

func (s *DocumentStore) Close(uri DocumentURI, diskText string) error {
	document, ok := s.documents[uri]
	if !ok {
		return ErrDocumentNotFound
	}
	if len(diskText) > s.limits.MaxFileBytes {
		return ErrFileTooLarge
	}
	document.Version, document.Text, document.Index, document.Open = 0, diskText, NewLineIndex(diskText), false
	s.documents[uri] = document
	return nil
}

func (s *DocumentStore) Delete(uri DocumentURI) error {
	if _, ok := s.documents[uri]; !ok {
		return ErrDocumentNotFound
	}
	delete(s.documents, uri)
	return nil
}

func (s *DocumentStore) Document(uri DocumentURI) (Document, bool) {
	document, ok := s.documents[uri]
	return document, ok
}

func (s *DocumentStore) Documents() []Document {
	out := make([]Document, 0, len(s.documents))
	for _, document := range s.documents {
		out = append(out, document)
	}
	return out
}

func (s *DocumentStore) validate(identity DocumentIdentity, text string) error {
	if strings.TrimSpace(string(identity.URI)) == "" || strings.TrimSpace(identity.ModulePath) == "" || strings.TrimSpace(identity.Path) == "" {
		return errors.New("document identity is incomplete")
	}
	if s.limits.MaxFiles > 0 {
		if _, exists := s.documents[identity.URI]; !exists && len(s.documents) >= s.limits.MaxFiles {
			return errors.New("workspace file limit reached")
		}
	}
	if s.limits.MaxFileBytes > 0 && len(text) > s.limits.MaxFileBytes {
		return ErrFileTooLarge
	}
	return nil
}
