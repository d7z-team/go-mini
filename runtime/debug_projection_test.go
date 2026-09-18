package runtime

import (
	"testing"
)

func TestExecutionSchemaProjectsUnwrappedResourceLimit(t *testing.T) {
	projected, err := ProjectExecutionError(ResourceLimitError{
		Code:    "execution.allocation_limit",
		Message: "execution allocation byte limit exceeded: max 1039",
	})
	if err != nil {
		t.Fatalf("ProjectExecutionError failed: %v", err)
	}
	if projected.Kind != ErrorRuntime || projected.Code != "execution.allocation_limit" {
		t.Fatalf("projected resource limit = (%s, %s), want (runtime, execution.allocation_limit)", projected.Kind, projected.Code)
	}
}
