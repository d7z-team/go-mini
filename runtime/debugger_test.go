package runtime

import "testing"

func TestDebuggerBoundsProjectedEventHistory(t *testing.T) {
	debugger := NewDebugger()
	for index := range maxDebugEvents + 10 {
		debugger.appendEvent(debugEvent{Kind: debugEventPanic, RunID: int64(index), Panic: newVMValue("Int", int64(index))})
	}
	events := debugger.Events()
	if len(events) != maxDebugEvents || events[0].RunID != 10 || events[len(events)-1].RunID != maxDebugEvents+9 {
		t.Fatalf("bounded events = first %d last %d count %d", events[0].RunID, events[len(events)-1].RunID, len(events))
	}
}
