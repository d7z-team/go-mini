package dap

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	protocol "github.com/google/go-dap"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func TestSessionCleansUpLaunchTargetWithoutProgram(t *testing.T) {
	cleaned := false
	session := NewSession(nil, &bytes.Buffer{}, func(context.Context, LaunchConfig) (LaunchTarget, error) {
		return LaunchTarget{Cleanup: func() error {
			cleaned = true
			return nil
		}}, nil
	})
	session.initialized = true
	_, err := session.handle(context.Background(), &protocol.LaunchRequest{Request: request(1, "launch"), Arguments: []byte(`{"cwd":"."}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !cleaned {
		t.Fatal("launcher cleanup was not called")
	}
}

func TestConfigurationDoneReportsStartFailureWithoutCommitting(t *testing.T) {
	program := compileTestProgram(t, "package main\nfunc main() {}\n")
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	session := NewSession(nil, &output, nil)
	session.initialized = true
	session.launched = true
	session.instance = instance
	_, err = session.handle(context.Background(), &protocol.ConfigurationDoneRequest{Request: request(1, "configurationDone")})
	if err != nil {
		t.Fatal(err)
	}
	if session.configured || session.execution != nil {
		t.Fatal("failed start committed DAP execution state")
	}
	message, err := protocol.ReadProtocolMessage(bufio.NewReader(&output))
	if err != nil {
		t.Fatal(err)
	}
	response, ok := message.(*protocol.ErrorResponse)
	if !ok || response.Success {
		t.Fatalf("start failure response = %#v", message)
	}
}

func TestSessionLaunchBreakpointAndInspect(t *testing.T) {
	root := t.TempDir()
	text := "package main\nfunc main() {\n\tvalues := []int{1, 2}\n\t_ = values[0]\n}\n"
	program := compileTestProgram(t, text)
	sources, err := workspace.NewTreeSourceSet("example", []workspace.TreeFile{{Path: "main.mgo", Text: text}})
	if err != nil {
		t.Fatal(err)
	}
	locations := &workspace.SourceLocations{}
	if err := locations.Add(root, sources); err != nil {
		t.Fatal(err)
	}
	workspaceRoot := t.TempDir()

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- NewSession(serverConn, serverConn, func(context.Context, LaunchConfig) (LaunchTarget, error) {
			return LaunchTarget{Program: program, RootPath: workspaceRoot, ModulePath: "example", Locations: locations}, nil
		}).Serve(context.Background())
	}()
	if err := clientConn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(clientConn)

	writeDAP(t, clientConn, &protocol.InitializeRequest{Request: request(1, "initialize"), Arguments: protocol.InitializeRequestArguments{LinesStartAt1: true, ColumnsStartAt1: true, PathFormat: "path"}})
	readDAP[*protocol.InitializeResponse](t, reader)
	writeDAP(t, clientConn, &protocol.LaunchRequest{Request: request(2, "launch"), Arguments: []byte(`{"cwd":"."}`)})
	readDAP[*protocol.LaunchResponse](t, reader)
	readDAP[*protocol.InitializedEvent](t, reader)

	sourcePath := filepath.Join(root, "main.mgo")
	writeDAP(t, clientConn, &protocol.SetBreakpointsRequest{Request: request(3, "setBreakpoints"), Arguments: protocol.SetBreakpointsArguments{Source: protocol.Source{Path: sourcePath}, Breakpoints: []protocol.SourceBreakpoint{{Line: 4}}}})
	breakpoints := readDAP[*protocol.SetBreakpointsResponse](t, reader)
	if len(breakpoints.Body.Breakpoints) != 1 || !breakpoints.Body.Breakpoints[0].Verified {
		t.Fatalf("breakpoints = %#v", breakpoints.Body.Breakpoints)
	}
	writeDAP(t, clientConn, &protocol.ConfigurationDoneRequest{Request: request(4, "configurationDone")})
	readDAP[*protocol.ConfigurationDoneResponse](t, reader)
	stopped := readDAP[*protocol.StoppedEvent](t, reader)
	if stopped.Body.Reason != "breakpoint" {
		t.Fatalf("stopped reason = %q", stopped.Body.Reason)
	}

	writeDAP(t, clientConn, &protocol.ThreadsRequest{Request: request(5, "threads")})
	threads := readDAP[*protocol.ThreadsResponse](t, reader)
	if len(threads.Body.Threads) != 1 {
		t.Fatalf("threads = %#v", threads.Body.Threads)
	}
	writeDAP(t, clientConn, &protocol.StackTraceRequest{Request: request(6, "stackTrace"), Arguments: protocol.StackTraceArguments{ThreadId: threads.Body.Threads[0].Id}})
	stack := readDAP[*protocol.StackTraceResponse](t, reader)
	if len(stack.Body.StackFrames) == 0 || stack.Body.StackFrames[0].Source.Path != sourcePath {
		t.Fatalf("stack = %#v", stack.Body.StackFrames)
	}
	writeDAP(t, clientConn, &protocol.ScopesRequest{Request: request(7, "scopes"), Arguments: protocol.ScopesArguments{FrameId: stack.Body.StackFrames[0].Id}})
	scopes := readDAP[*protocol.ScopesResponse](t, reader)
	if len(scopes.Body.Scopes) == 0 {
		t.Fatal("missing scopes")
	}
	writeDAP(t, clientConn, &protocol.VariablesRequest{Request: request(8, "variables"), Arguments: protocol.VariablesArguments{VariablesReference: scopes.Body.Scopes[0].VariablesReference, Start: 0, Count: 1}})
	variables := readDAP[*protocol.VariablesResponse](t, reader)
	if len(variables.Body.Variables) != 1 {
		t.Fatalf("variables = %#v", variables.Body.Variables)
	}
	oldReference := scopes.Body.Scopes[0].VariablesReference

	writeDAP(t, clientConn, &protocol.ContinueRequest{Request: request(9, "continue"), Arguments: protocol.ContinueArguments{ThreadId: threads.Body.Threads[0].Id}})
	readDAP[*protocol.ContinueResponse](t, reader)
	readDAP[*protocol.ContinuedEvent](t, reader)
	for {
		message, err := protocol.ReadProtocolMessage(reader)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := message.(*protocol.TerminatedEvent); ok {
			break
		}
	}
	writeDAP(t, clientConn, &protocol.VariablesRequest{Request: request(10, "variables"), Arguments: protocol.VariablesArguments{VariablesReference: oldReference}})
	stale := readDAP[*protocol.ErrorResponse](t, reader)
	if stale.Success {
		t.Fatal("stale variable reference succeeded")
	}
	writeDAP(t, clientConn, &protocol.DisconnectRequest{Request: request(11, "disconnect"), Arguments: &protocol.DisconnectArguments{}})
	readDAP[*protocol.DisconnectResponse](t, reader)
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DAP server did not stop")
	}
}

func TestSessionReportsExecutionFailure(t *testing.T) {
	root := t.TempDir()
	program := compileTestProgram(t, "package main\nfunc main() { panic(\"boom\") }\n")
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	done := make(chan error, 1)
	go func() {
		done <- NewSession(serverConn, serverConn, func(context.Context, LaunchConfig) (LaunchTarget, error) {
			return LaunchTarget{Program: program, RootPath: root, ModulePath: "example"}, nil
		}).Serve(context.Background())
	}()
	if err := clientConn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(clientConn)
	writeDAP(t, clientConn, &protocol.InitializeRequest{Request: request(1, "initialize"), Arguments: protocol.InitializeRequestArguments{LinesStartAt1: true, ColumnsStartAt1: true}})
	readDAP[*protocol.InitializeResponse](t, reader)
	writeDAP(t, clientConn, &protocol.LaunchRequest{Request: request(2, "launch"), Arguments: []byte(`{"cwd":"."}`)})
	readDAP[*protocol.LaunchResponse](t, reader)
	readDAP[*protocol.InitializedEvent](t, reader)
	writeDAP(t, clientConn, &protocol.ConfigurationDoneRequest{Request: request(3, "configurationDone")})
	readDAP[*protocol.ConfigurationDoneResponse](t, reader)
	output := readDAP[*protocol.OutputEvent](t, reader)
	if output.Body.Category != "stderr" || output.Body.Output == "" {
		t.Fatalf("failure output = %#v", output.Body)
	}
	exited := readDAP[*protocol.ExitedEvent](t, reader)
	if exited.Body.ExitCode != 1 {
		t.Fatalf("exit code = %d", exited.Body.ExitCode)
	}
	readDAP[*protocol.TerminatedEvent](t, reader)
	writeDAP(t, clientConn, &protocol.DisconnectRequest{Request: request(4, "disconnect"), Arguments: &protocol.DisconnectArguments{}})
	readDAP[*protocol.DisconnectResponse](t, reader)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DAP server did not stop")
	}
}

func TestSessionFileURIPath(t *testing.T) {
	root := t.TempDir()
	session := NewSession(nil, nil, nil)
	session.pathFormat = "uri"
	session.target = LaunchTarget{RootPath: root, ModulePath: "example"}
	path := filepath.Join(root, "pkg", "main.mgo")
	modulePath, file, err := session.sourceIdentity(protocol.Source{Path: session.clientPath(path)})
	if err != nil {
		t.Fatal(err)
	}
	if modulePath != "example/pkg" || file != "pkg/main.mgo" {
		t.Fatalf("source identity = %q, %q", modulePath, file)
	}
}

func TestSessionKeepsCodeOnlyDebugErrorsNonterminal(t *testing.T) {
	program := compileTestProgram(t, "package main\nfunc main() { for {} }\n").WithoutSymbols()
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	execution, err := instance.StartMain()
	if err != nil {
		t.Fatal(err)
	}
	if state, err := execution.Poll(); err != nil || state != minigoruntime.ExecutionRunning {
		t.Fatalf("initial poll = %s, %v", state, err)
	}
	if err := execution.RequestPause(); err != nil {
		t.Fatal(err)
	}
	if state, err := execution.Poll(); err != nil || state != minigoruntime.ExecutionPaused {
		t.Fatalf("pause poll = %s, %v", state, err)
	}
	snapshot, err := execution.DebugSnapshot()
	if err != nil || len(snapshot.Threads) == 0 {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}

	var output bytes.Buffer
	session := NewSession(nil, &output, nil)
	session.initialized = true
	session.launched = true
	session.target = LaunchTarget{RootPath: t.TempDir(), ModulePath: "example"}
	session.instance = instance
	session.execution = execution
	_, err = session.handle(context.Background(), &protocol.StackTraceRequest{
		Request: request(1, "stackTrace"), Arguments: protocol.StackTraceArguments{ThreadId: int(snapshot.Threads[0].ID)},
	})
	if err != nil {
		t.Fatal(err)
	}
	stack := readDAP[*protocol.StackTraceResponse](t, bufio.NewReader(&output))
	if len(stack.Body.StackFrames) == 0 || stack.Body.StackFrames[0].Source != nil || stack.Body.StackFrames[0].Line != 0 || stack.Body.StackFrames[0].Column != 0 {
		t.Fatalf("code-only stack = %#v", stack.Body.StackFrames)
	}

	output.Reset()
	_, err = session.handle(context.Background(), &protocol.StepInRequest{
		Request: request(2, "stepIn"), Arguments: protocol.StepInArguments{ThreadId: int(snapshot.Threads[0].ID)},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := readDAP[*protocol.ErrorResponse](t, bufio.NewReader(&output))
	if response.Success || session.terminated || execution.State() != minigoruntime.ExecutionPaused {
		t.Fatalf("code-only step response = %#v, terminated=%t state=%s", response, session.terminated, execution.State())
	}
	if _, err := execution.Continue(); err != nil {
		t.Fatal(err)
	}
	execution.Cancel()
}

func compileTestProgram(t *testing.T, text string) *minigoruntime.Program {
	t.Helper()
	set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: text}}}})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := compiler.Prepare(compiler.Request{Root: "example", Sources: set, Symbols: true})
	if err != nil || !prepared.Checked.OK() || prepared.Image == nil || prepared.Symbols == nil {
		t.Fatalf("prepare: result=%#v err=%v", prepared.Checked.Diagnostics, err)
	}
	program, err := minigoruntime.LoadExecutionImage(*prepared.Image)
	if err != nil {
		t.Fatal(err)
	}
	program, err = program.WithSymbols(*prepared.Symbols)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func request(seq int, command string) protocol.Request {
	return protocol.Request{ProtocolMessage: protocol.ProtocolMessage{Seq: seq, Type: "request"}, Command: command}
}

func writeDAP(t *testing.T, writer net.Conn, message protocol.Message) {
	t.Helper()
	if err := protocol.WriteProtocolMessage(writer, message); err != nil {
		t.Fatal(err)
	}
}

func readDAP[T protocol.Message](t *testing.T, reader *bufio.Reader) T {
	t.Helper()
	message, err := protocol.ReadProtocolMessage(reader)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := message.(T)
	if !ok {
		t.Fatalf("message = %T, want %T", message, *new(T))
	}
	return value
}
