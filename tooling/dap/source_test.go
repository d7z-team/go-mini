package dap

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	minigoruntime "github.com/d7z-team/mini-go/runtime"
	protocol "github.com/google/go-dap"
)

func TestHistoricalSourceUsesPausedFrameIdentityAfterPatch(t *testing.T) {
	oldText := "package main\nfunc main() { x := 0; for { x++ } }\n"
	newText := "package main\nfunc main() { x := 1; for { x += 2 } }\n"
	old, next := compileTestProgram(t, oldText), compileTestProgram(t, newText)
	instance, err := old.Instantiate(t.Context(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	execution, err := instance.StartMain()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := execution.PollSteps(1); err != nil {
		t.Fatal(err)
	}
	if err := execution.RequestPause(); err != nil {
		t.Fatal(err)
	}
	if state, err := execution.Poll(); err != nil || state != minigoruntime.ExecutionPaused {
		t.Fatalf("pause: %v %v", state, err)
	}
	plan, err := instance.PreparePatch(t.Context(), next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.ApplyPatch(plan); err != nil {
		t.Fatal(err)
	}
	snapshot, err := execution.DebugSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	frame := snapshot.Frames[0]
	if frame.Generation != 1 || frame.ProgramHash != old.Hash() || frame.SourceHash == "" || frame.SymbolsHash != old.SymbolsHash() || frame.ScopeID == 0 {
		t.Fatalf("frame: %+v", frame)
	}
	var output bytes.Buffer
	session := NewSession(nil, &output, nil)
	session.initialized = true
	session.instance = instance
	session.execution = execution
	session.target = LaunchTarget{RootPath: t.TempDir(), ModulePath: "example", Source: func(_ context.Context, id SourceIdentity) (string, error) {
		if id.ProgramHash != old.Hash() {
			t.Fatalf("source identity: %+v", id)
		}
		return oldText, nil
	}}
	_, err = session.handle(t.Context(), &protocol.StackTraceRequest{Request: request(1, "stackTrace"), Arguments: protocol.StackTraceArguments{ThreadId: int(frame.ThreadID)}})
	if err != nil {
		t.Fatal(err)
	}
	stack := readDAP[*protocol.StackTraceResponse](t, bufio.NewReader(&output))
	if !strings.Contains(stack.Body.StackFrames[0].Name, "generation 1") {
		t.Fatalf("stack: %+v", stack)
	}
	source := stack.Body.StackFrames[0].Source
	if module, file, err := session.sourceIdentity(*source); err != nil || module != "example" || file != "main.mgo" {
		t.Fatalf("breakpoint identity: %s %s %v", module, file, err)
	}
	output.Reset()
	_, err = session.handle(t.Context(), &protocol.SourceRequest{Request: request(2, "source"), Arguments: protocol.SourceArguments{SourceReference: source.SourceReference}})
	if err != nil {
		t.Fatal(err)
	}
	response := readDAP[*protocol.SourceResponse](t, bufio.NewReader(&output))
	if response.Body.Content != oldText {
		t.Fatalf("source: %+v", response)
	}
	session.target.Source = func(context.Context, SourceIdentity) (string, error) { return newText, nil }
	if _, err := session.sourceContent(t.Context(), source.SourceReference); err == nil {
		t.Fatal("mismatched provider source accepted")
	}
	session.target.Source = nil
	path := filepath.Join(session.target.RootPath, "main.mgo")
	if err := os.WriteFile(path, []byte(newText), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := session.sourceContent(t.Context(), source.SourceReference); err == nil {
		t.Fatal("current disk source shown for historical frame")
	}
	if err := os.WriteFile(path, []byte(oldText), 0o600); err != nil {
		t.Fatal(err)
	}
	if text, err := session.sourceContent(t.Context(), source.SourceReference); err != nil || text != oldText {
		t.Fatalf("verified disk source: %q %v", text, err)
	}
	if _, err := execution.Continue(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.sourceContent(t.Context(), source.SourceReference); err == nil {
		t.Fatal("source reference survived resume")
	}
	execution.Cancel()
}
