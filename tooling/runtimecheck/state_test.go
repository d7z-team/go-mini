package runtimecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStateVectorsPreserveFailedStartAndDeterministicReuse(t *testing.T) {
	input := []byte(`{"module":{"path":"test","package":"main"},"constants":[{"id":"answer","type":{"kind":3,"primitive":3},"value":42}],"functions":[{"id":"fn.Main","signature":{"results":[{"kind":3,"primitive":3}]},"instructions":[{"op":"const","payload":{"constant":"answer"}},{"op":"return","payload":{"result_count":1}}]}]}`)
	inputs := map[string][]byte{"entry": input}
	encoded, err := GenerateStateVectors(context.Background(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := GenerateStateVectors(context.Background(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, repeated) {
		t.Fatal("same owner actions produced different observations")
	}
	var vectors []StateVector
	if err := json.Unmarshal(encoded, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, vector := range vectors {
		if vector.Limit != 128 {
			continue
		}
		if vector.Actions[0].Error != "execution.allocation_limit" || vector.Actions[1].Operation != "start" || vector.Actions[1].Error != "" {
			t.Fatalf("failed frame preparation and subsequent reuse: %+v", vector.Actions)
		}
		last := vector.Actions[len(vector.Actions)-1]
		if last.State != "completed" || last.Memory != [4]int64{128, 128, 384, 128} {
			t.Fatalf("reused frame result: %+v", last)
		}
		canceled := false
		for _, action := range vector.Actions {
			if action.Operation == "cancel" {
				canceled = action.Error == ""
			}
		}
		if !canceled {
			t.Fatal("missing successful cancellation before reuse")
		}
		return
	}
	t.Fatal("missing constrained invocation")
}

func TestStateVectorsRejectMalformedInputBeforePublication(t *testing.T) {
	for _, input := range []string{
		`{"functions":`,
		`[]`,
		`[{"module":{"path":"test","package":"main"}},{"module":{"path":"test","package":"main"}}]`,
	} {
		encoded, err := GenerateStateVectors(context.Background(), map[string][]byte{"broken": []byte(input)})
		if err == nil || !strings.Contains(err.Error(), "broken") || encoded != nil {
			t.Fatalf("input %q, data %q, error %v", input, encoded, err)
		}
	}
}
