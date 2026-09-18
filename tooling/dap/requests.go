package dap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	protocol "github.com/google/go-dap"

	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func (s *Session) handle(ctx context.Context, message protocol.Message) (bool, error) {
	request, ok := message.(protocol.RequestMessage)
	if !ok {
		return false, nil
	}
	base := request.GetRequest()
	if !s.initialized && base.Command != "initialize" {
		return false, s.sendError(base, "debug adapter is not initialized")
	}
	switch value := message.(type) {
	case *protocol.SourceRequest:
		text, err := s.sourceContent(ctx, value.Arguments.SourceReference)
		if err != nil {
			return false, s.sendError(base, err.Error())
		}
		return false, s.send(&protocol.SourceResponse{Response: s.response(base), Body: protocol.SourceResponseBody{Content: text, MimeType: "text/plain"}})
	case *protocol.InitializeRequest:
		if s.initialized {
			return false, s.sendError(base, "debug adapter is already initialized")
		}
		s.initialized = true
		s.linesStartAt1, s.columnsStartAt1 = value.Arguments.LinesStartAt1, value.Arguments.ColumnsStartAt1
		if value.Arguments.PathFormat != "" {
			if value.Arguments.PathFormat != "path" && value.Arguments.PathFormat != "uri" {
				return false, s.sendError(base, "pathFormat must be path or uri")
			}
			s.pathFormat = value.Arguments.PathFormat
		}
		return false, s.send(&protocol.InitializeResponse{Response: s.response(base), Body: protocol.Capabilities{SupportsConfigurationDoneRequest: true, SupportTerminateDebuggee: true, SupportsTerminateRequest: true}})
	case *protocol.LaunchRequest:
		if s.launched {
			return false, s.sendError(base, "debug target is already launched")
		}
		var config LaunchConfig
		if err := json.Unmarshal(value.Arguments, &config); err != nil {
			return false, s.sendError(base, "invalid launch arguments: "+err.Error())
		}
		if strings.TrimSpace(config.Directory) == "" {
			config.Directory = "."
		}
		if s.launcher == nil {
			return false, s.sendError(base, "debug launcher is not configured")
		}
		target, err := s.launcher(ctx, config)
		if err != nil {
			return false, s.sendError(base, err.Error())
		}
		if target.Program == nil {
			if target.Cleanup != nil {
				_ = target.Cleanup()
			}
			return false, s.sendError(base, "launcher returned a nil program")
		}
		target.Options.Debugger = minigoruntime.NewDebugger()
		instance, err := target.Program.Instantiate(context.Background(), target.Options)
		if err != nil {
			if target.Cleanup != nil {
				_ = target.Cleanup()
			}
			return false, s.sendError(base, err.Error())
		}
		s.target, s.instance, s.launched = target, instance, true
		if err := s.send(&protocol.LaunchResponse{Response: s.response(base)}); err != nil {
			return false, err
		}
		return false, s.send(&protocol.InitializedEvent{Event: s.event("initialized")})
	case *protocol.SetBreakpointsRequest:
		if !s.launched {
			return false, s.sendError(base, "debug target is not launched")
		}
		modulePath, file, err := s.sourceIdentity(value.Arguments.Source)
		if err != nil {
			return false, s.sendError(base, err.Error())
		}
		lines := make([]int, 0, len(value.Arguments.Breakpoints))
		for _, breakpoint := range value.Arguments.Breakpoints {
			lines = append(lines, s.runtimeLine(breakpoint.Line))
		}
		if len(value.Arguments.Breakpoints) == 0 {
			for _, line := range value.Arguments.Lines {
				lines = append(lines, s.runtimeLine(line))
			}
		}
		resolved, err := s.instance.SetBreakpoints(modulePath, file, lines)
		if err != nil {
			return false, s.sendError(base, err.Error())
		}
		breakpoints := make([]protocol.Breakpoint, len(resolved))
		for index, breakpoint := range resolved {
			s.breakpointID++
			breakpoints[index] = protocol.Breakpoint{Id: s.breakpointID, Verified: breakpoint.Verified, Line: s.clientLine(breakpoint.Line), Column: s.clientColumn(breakpoint.Column), Source: &value.Arguments.Source}
			if !breakpoint.Verified {
				breakpoints[index].Message = "breakpoint is not currently resolvable"
			}
		}
		return false, s.send(&protocol.SetBreakpointsResponse{Response: s.response(base), Body: protocol.SetBreakpointsResponseBody{Breakpoints: breakpoints}})
	case *protocol.ConfigurationDoneRequest:
		if !s.launched {
			return false, s.sendError(base, "debug target is not launched")
		}
		if s.configured {
			return false, s.sendError(base, "configuration is already complete")
		}
		execution, err := s.instance.StartMain()
		if err != nil {
			return false, s.failRequest(base, err)
		}
		s.execution, s.configured = execution, true
		if err := s.send(&protocol.ConfigurationDoneResponse{Response: s.response(base)}); err != nil {
			return false, err
		}
		return false, s.publishState()
	case *protocol.ThreadsRequest:
		threads := []protocol.Thread{{Id: 1, Name: "Mini-Go"}}
		if snapshot, err := s.debugSnapshot(); err == nil {
			threads = make([]protocol.Thread, len(snapshot.Threads))
			for index, thread := range snapshot.Threads {
				threads[index] = protocol.Thread{Id: int(thread.ID), Name: thread.Name}
			}
		}
		return false, s.send(&protocol.ThreadsResponse{Response: s.response(base), Body: protocol.ThreadsResponseBody{Threads: threads}})
	case *protocol.StackTraceRequest:
		snapshot, err := s.debugSnapshot()
		if err != nil {
			return false, s.sendError(base, err.Error())
		}
		frames := make([]protocol.StackFrame, 0, len(snapshot.Frames))
		for _, frame := range snapshot.Frames {
			if int(frame.ThreadID) != value.Arguments.ThreadId {
				continue
			}
			stackFrame := protocol.StackFrame{Id: frame.ID, Name: fmt.Sprintf("%s [generation %d, scope %d, %s]", frame.FunctionID, frame.Generation, frame.ScopeID, frame.ProgramHash), ModuleId: frame.ModulePath}
			if frame.HasSymbols && frame.File != "" {
				stackFrame.Source = s.frameSource(frame, snapshot.Epoch)
				stackFrame.Line = s.clientLine(frame.Line)
				stackFrame.Column = s.clientColumn(frame.Column)
			}
			frames = append(frames, stackFrame)
		}
		start, end := page(value.Arguments.StartFrame, value.Arguments.Levels, len(frames))
		return false, s.send(&protocol.StackTraceResponse{Response: s.response(base), Body: protocol.StackTraceResponseBody{StackFrames: frames[start:end], TotalFrames: len(frames)}})
	case *protocol.ScopesRequest:
		if s.execution == nil {
			return false, s.sendError(base, "execution is not paused")
		}
		scopes, err := s.execution.DebugScopes(value.Arguments.FrameId)
		if err != nil {
			return false, s.sendError(base, err.Error())
		}
		result := make([]protocol.Scope, len(scopes))
		for index, scope := range scopes {
			result[index] = protocol.Scope{Name: scope.Name, PresentationHint: strings.ToLower(scope.Name), VariablesReference: scope.VariablesReference, NamedVariables: scope.NamedVariables}
		}
		return false, s.send(&protocol.ScopesResponse{Response: s.response(base), Body: protocol.ScopesResponseBody{Scopes: result}})
	case *protocol.VariablesRequest:
		if s.execution == nil {
			return false, s.sendError(base, "execution is not paused")
		}
		variables, err := s.execution.DebugVariables(value.Arguments.VariablesReference, value.Arguments.Start, value.Arguments.Count)
		if err != nil {
			return false, s.sendError(base, err.Error())
		}
		result := make([]protocol.Variable, len(variables))
		for index, variable := range variables {
			result[index] = protocol.Variable{Name: variable.Name, Value: variable.Value, Type: variable.Type, VariablesReference: variable.VariablesReference, IndexedVariables: variable.IndexedVariables, NamedVariables: variable.NamedVariables}
		}
		return false, s.send(&protocol.VariablesResponse{Response: s.response(base), Body: protocol.VariablesResponseBody{Variables: result}})
	case *protocol.ContinueRequest:
		if err := s.requirePaused(); err != nil {
			return false, s.sendError(base, err.Error())
		}
		_, resumeErr := s.execution.Continue()
		if resumeErr != nil {
			return false, s.failRequest(base, resumeErr)
		}
		if err := s.send(&protocol.ContinueResponse{Response: s.response(base), Body: protocol.ContinueResponseBody{AllThreadsContinued: true}}); err != nil {
			return false, err
		}
		if err := s.send(&protocol.ContinuedEvent{Event: s.event("continued"), Body: protocol.ContinuedEventBody{ThreadId: value.Arguments.ThreadId, AllThreadsContinued: true}}); err != nil {
			return false, err
		}
		return false, s.publishState()
	case *protocol.NextRequest:
		return false, s.step(base, value.Arguments.ThreadId, "next")
	case *protocol.StepInRequest:
		return false, s.step(base, value.Arguments.ThreadId, "stepIn")
	case *protocol.StepOutRequest:
		return false, s.step(base, value.Arguments.ThreadId, "stepOut")
	case *protocol.PauseRequest:
		if s.execution == nil {
			return false, s.sendError(base, "execution is not running")
		}
		if err := s.execution.RequestPause(); err != nil {
			return false, s.sendError(base, err.Error())
		}
		return false, s.send(&protocol.PauseResponse{Response: s.response(base)})
	case *protocol.TerminateRequest:
		if s.execution != nil {
			s.execution.Cancel()
		}
		if err := s.send(&protocol.TerminateResponse{Response: s.response(base)}); err != nil {
			return false, err
		}
		return false, s.terminate()
	case *protocol.DisconnectRequest:
		if s.execution != nil && value.Arguments.TerminateDebuggee {
			s.execution.Cancel()
		}
		if err := s.send(&protocol.DisconnectResponse{Response: s.response(base)}); err != nil {
			return false, err
		}
		if err := s.terminate(); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, s.sendError(base, "unsupported request "+base.Command)
	}
}

func (s *Session) step(request *protocol.Request, threadID int, command string) error {
	if err := s.requirePaused(); err != nil {
		return s.sendError(request, err.Error())
	}
	var resumeErr error
	switch command {
	case "next":
		_, resumeErr = s.execution.StepOver()
	case "stepIn":
		_, resumeErr = s.execution.StepInto()
	case "stepOut":
		_, resumeErr = s.execution.StepOut()
	}
	if resumeErr != nil {
		return s.failRequest(request, resumeErr)
	}
	switch command {
	case "next":
		if err := s.send(&protocol.NextResponse{Response: s.response(request)}); err != nil {
			return err
		}
	case "stepIn":
		if err := s.send(&protocol.StepInResponse{Response: s.response(request)}); err != nil {
			return err
		}
	case "stepOut":
		if err := s.send(&protocol.StepOutResponse{Response: s.response(request)}); err != nil {
			return err
		}
	}
	if err := s.send(&protocol.ContinuedEvent{Event: s.event("continued"), Body: protocol.ContinuedEventBody{ThreadId: threadID, AllThreadsContinued: true}}); err != nil {
		return err
	}
	return s.publishState()
}

func (s *Session) failRequest(request *protocol.Request, err error) error {
	if sendErr := s.sendError(request, err.Error()); sendErr != nil {
		return sendErr
	}
	if s.execution != nil && s.execution.State() == minigoruntime.ExecutionPaused {
		return nil
	}
	return s.failExecution(err)
}
