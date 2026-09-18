package service

import (
	"context"
	"errors"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/language"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestInputAnalysisTransactionsAndSnapshotLifetime(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "app", Files: []source.File{{Path: "main.mgo", Text: "package app\nfunc Answer() int {return 42}\n"}}}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := New(t.Context(), language.Config{Root: "app", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	initial, err := session.Analyze(t.Context(), "1")
	if err != nil {
		t.Fatal(err)
	}
	identity := language.DocumentIdentity{URI: "mini-go://app/main.mgo", ModulePath: "app", Path: "main.mgo"}
	revision, err := session.Update(t.Context(), nil, []language.DocumentUpdate{{Operation: "open", Identity: identity, Version: 1, Text: "package app\nfunc Answer() int {return 43}\n"}})
	if err != nil || revision != "2" {
		t.Fatalf("open: %s, %v", revision, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = session.Analyze(ctx, revision); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err = session.Query(t.Context(), Query{Snapshot: initial.Snapshot, Operation: "hover", URI: identity.URI, Position: language.Position{Line: 1, Character: 6}}); err != nil {
		t.Fatal(err)
	}
	current, err := session.Analyze(t.Context(), revision)
	if err != nil {
		t.Fatal(err)
	}
	if current.Snapshot == initial.Snapshot {
		t.Fatal("analysis did not advance snapshot")
	}
	if _, err = session.Query(t.Context(), Query{Snapshot: initial.Snapshot, Operation: "hover"}); !errors.Is(err, ErrStale) {
		t.Fatalf("old snapshot: %v", err)
	}
	if _, err = session.Update(t.Context(), nil, []language.DocumentUpdate{{Operation: "change", Identity: identity, Version: 2, Changes: []language.ContentChange{{Text: "package app"}}}, {Operation: "change", Identity: identity, Version: 1}}); err == nil {
		t.Fatal("rollback accepted")
	}
	again, err := session.Analyze(t.Context(), revision)
	if err != nil || again.Snapshot != current.Snapshot {
		t.Fatalf("failed batch modified revision: %+v %v", again, err)
	}
	session.Close()
	session.Close()
	if _, err = session.Analyze(t.Context(), revision); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestSessionOwnsTargetConfiguration(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "app", Files: []source.File{{Path: "main.mgo", Text: "package app\nfunc Answer() int {return 42}"}}}})
	if err != nil {
		t.Fatal(err)
	}
	tags := []string{"feature"}
	session, err := New(t.Context(), language.Config{Root: "app", Sources: sources, Target: target.Target{Tags: tags}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tags[0] = "invalid!"
	build, err := session.Build(t.Context(), BuildOptions{Revision: "1", EntryPoints: []compiler.EntryPoint{{Name: "default", ModulePath: "app", Function: "Answer"}}})
	if err != nil || build.Result.Image == nil {
		t.Fatalf("caller changed session target: %v %v", err, build.Result.Checked.Diagnostics)
	}
	if err := target.Validate(build.Target); err != nil {
		t.Fatal(err)
	}
	build.Target.Tags[0] = "changed"
	if !session.config.Target.Has("feature") {
		t.Fatal("build result exposed session target")
	}
}
