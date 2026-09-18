package runtime

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestSharedStepLimits(t *testing.T) {
	data, err := os.ReadFile("../testdata/runtime/step_limits.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input, Normalized string
		Error             bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(test.Input, func(t *testing.T) {
			input, err := strconv.ParseInt(test.Input, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			limits := Limits{MaxSteps: input}
			if err := validateLimits(limits); (err != nil) != test.Error {
				t.Fatal(err)
			}
			if !test.Error && strconv.FormatInt(normalizeLimits(limits).MaxSteps, 10) != test.Normalized {
				t.Fatal("normalization mismatch")
			}
		})
	}
}

func TestUnlimitedStepsPreservePollCountsAndSampling(t *testing.T) {
	artifact := ir.NewArtifact("budget/loop", "main")
	artifact.Functions = []ir.Function{{ID: "fn.entry", Signature: testSignature("function() Void"), Instructions: []ir.Instruction{
		{Op: string(ir.OpLabel), Payload: testPayload(ir.LabelPayload{Label: "loop"})},
		{Op: string(ir.OpJump), Payload: testPayload(ir.JumpPayload{Label: "loop"})},
	}}}
	instance, err := patchTestProgram(t, artifact, "budget-loop").Instantiate(t.Context(), InstanceOptions{Limits: Limits{MaxSteps: UnlimitedSteps}, GuestProfile: GuestProfileOptions{SampleEvery: 4, MaxEntries: 8}})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	execution, err := instance.Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.vm.enterOwner(); err != nil {
		t.Fatal(err)
	}
	instance.vm.executedSteps = math.MaxInt64 - 1
	instance.vm.machine.foreground.budget.steps = math.MaxInt64 - 1
	instance.vm.leaveOwner()
	for range 3 {
		state, steps, err := execution.PollSteps(7)
		if err != nil || state != ExecutionRunning || steps != 7 {
			t.Fatalf("poll = %s %d %v", state, steps, err)
		}
	}
	stats, err := execution.ScopeStats(t.Context())
	if err != nil || stats.Steps != math.MaxInt64 {
		t.Fatalf("stats = %#v, %v", stats, err)
	}
	var sampled uint64
	for _, sample := range execution.GuestProfile().Samples {
		sampled += sample.Count
	}
	if sampled != 5 {
		t.Fatalf("saturated total changed sample interval: %d", sampled)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
}
