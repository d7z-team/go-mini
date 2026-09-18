package runtime

import "math"

type deferredCall struct {
	caller *moduleInstance
	ref    functionRef
}

type executionMachine struct {
	vm                 *vm
	foreground         *executionScope
	scopes             map[int64]*executionScope
	runnable           []*executionTask
	runnableHead       int
	blocked            []*executionTask
	running            *executionTask
	paused             *executionTask
	pollBudget         int
	pollAttempts       int
	pollExecuted       int
	blockedWakePending bool
}

func (machine *executionMachine) runnableCount() int {
	if machine == nil {
		return 0
	}
	return len(machine.runnable) - machine.runnableHead
}

func (machine *executionMachine) runnableTasks() []*executionTask {
	if machine == nil || machine.runnableHead >= len(machine.runnable) {
		return nil
	}
	return machine.runnable[machine.runnableHead:]
}

func (machine *executionMachine) pushRunnable(tasks ...*executionTask) {
	if len(tasks) == 0 {
		return
	}
	active := machine.runnableCount()
	if active == 0 {
		clear(machine.runnable)
		machine.runnable = machine.runnable[:0]
		machine.runnableHead = 0
	} else if machine.runnableHead > 0 && active+len(tasks) <= cap(machine.runnable) {
		copy(machine.runnable, machine.runnable[machine.runnableHead:])
		clear(machine.runnable[active:])
		machine.runnable = machine.runnable[:active]
		machine.runnableHead = 0
	}
	machine.runnable = append(machine.runnable, tasks...)
}

func (machine *executionMachine) pushRunnableFront(task *executionTask) {
	if machine.runnableHead > 0 {
		machine.runnableHead--
		machine.runnable[machine.runnableHead] = task
		return
	}
	machine.runnable = append(machine.runnable, nil)
	copy(machine.runnable[1:], machine.runnable[:len(machine.runnable)-1])
	machine.runnable[0] = task
}

func (machine *executionMachine) popRunnable() *executionTask {
	if machine.runnableCount() == 0 {
		return nil
	}
	task := machine.runnable[machine.runnableHead]
	machine.runnable[machine.runnableHead] = nil
	machine.runnableHead++
	if machine.runnableHead == len(machine.runnable) {
		machine.runnable = machine.runnable[:0]
		machine.runnableHead = 0
	}
	return task
}

type executionScope struct {
	id            int64
	rootID        int64
	program       bool
	execution     *Execution
	budget        *executionBudget
	started       RevisionInfo
	rootState     ExecutionState
	rootPublished bool
	tasks         int
	timers        int
	ffiCalls      int
	err           error
	done          chan struct{}
	settled       bool
}

type executionTask struct {
	id           int64
	scope        *executionScope
	execution    *Execution
	budget       *executionBudget
	frames       []*executionFrame
	debugParents []debugFrame
	blocked      *blockedOperation
	pendingErr   error
	terminal     func([]vmValue, error)
	finished     bool
}

func (machine *executionMachine) attachExecution(scopeID int64, execution *Execution) {
	if machine == nil || scopeID == 0 || execution == nil {
		return
	}
	scope := machine.scopes[scopeID]
	if scope == nil {
		return
	}
	scope.execution = execution
	execution.mu.Lock()
	execution.scopeDone = scope.done
	execution.mu.Unlock()
	for _, task := range machine.runnableTasks() {
		if task.scope == scope {
			task.execution = execution
		}
	}
	for _, task := range machine.blocked {
		if task.scope == scope {
			task.execution = execution
		}
	}
	if machine.paused != nil && machine.paused.scope == scope {
		machine.paused.execution = execution
	}
}

func (task *executionTask) inNoSwitchRegion() bool {
	if task == nil {
		return false
	}
	for index := len(task.frames) - 1; index >= 0; index-- {
		active := task.frames[index]
		if active != nil && active.frame != nil && active.frame.function.Decl.NoSwitch {
			return true
		}
	}
	return false
}

type executionBudget struct {
	steps        int64
	profilePhase uint64
}

func (task *executionTask) consumeStep(limit int64) error {
	if task.budget == nil {
		task.budget = &executionBudget{}
	}
	if limit > 0 && task.budget.steps >= limit {
		return StepLimitError{MaxSteps: limit}
	}
	if task.budget.steps < math.MaxInt64 {
		task.budget.steps++
	}
	task.budget.profilePhase++
	return nil
}

type executionFrame struct {
	frame           *frame
	expectedResults int
	qualifyResults  bool
	resume          func(*executionTask, *executionFrame, []vmValue) error
	abort           func()
	deferred        bool
	recoveredPanic  *machinePanic
	completion      *frameCompletion
	pinnedRevision  *instanceRevision
}

type artifactCallbackRequest struct {
	module        *moduleInstance
	functionID    string
	args          []vmValue
	upvalues      map[string]*slot
	resultCount   int
	resumeResults int
	resume        func([]vmValue) ([]vmValue, error)
}

type reflectRecvRequest struct {
	ctx      intrinsicContext
	waitable vmValue
}

type reflectSendRequest struct {
	ctx      intrinsicContext
	waitable vmValue
	value    vmValue
}

type reflectSelectRequest struct {
	waitSet  vmValue
	complete func(int) ([]vmValue, error)
}

func (request *reflectRecvRequest) Error() string {
	return "reflect receive blocked"
}

func (request *reflectSendRequest) Error() string {
	return "reflect send blocked"
}

func (request *reflectSelectRequest) Error() string {
	return "reflect select blocked"
}

func (request *artifactCallbackRequest) Error() string {
	if request == nil {
		return "nil artifact callback request"
	}
	return "artifact callback request for " + request.functionID
}

type frameCompletion struct {
	returnValues []vmValue
	panic        *machinePanic
	resume       bool
}

type machinePanic struct {
	err        panicError
	runtimeErr Error
	debugEvent *debugEvent
}

type blockedOperation struct {
	kind       string
	resource   *waitableResource
	waitable   vmValue
	waitSet    vmValue
	ffi        *pendingFFICall
	withOK     bool
	recvDone   func(vmValue, bool) ([]vmValue, error)
	recvToken  vmValue
	sendDone   func() []vmValue
	selectDone func(int) ([]vmValue, error)
	error      Error
}

type taskYield struct {
	kind  string
	child *executionTask
}
