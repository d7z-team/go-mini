package rpc

import (
	"strings"
	"testing"
)

func TestCheckMethodSupport(t *testing.T) {
	first := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
	second := first
	second.ContractHash = strings.Repeat("b", 64)
	mismatch := first
	mismatch.ContractHash = strings.Repeat("c", 64)
	missing := first
	missing.ID, missing.Name = "example::Service.Other", "Other"
	for _, test := range []struct {
		name     string
		required []Method
		code     Code
	}{
		{"empty", nil, ""},
		{"multiple_contracts", []Method{first, second}, ""},
		{"missing", []Method{missing}, CodeUnimplemented},
		{"mismatch", []Method{mismatch}, CodeFailedPrecondition},
		{"missing_then_mismatch", []Method{missing, mismatch}, CodeFailedPrecondition},
		{"mismatch_then_missing", []Method{mismatch, missing}, CodeFailedPrecondition},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := CheckMethodSupport([]Method{first, second}, test.required); codeOf(err) != test.code {
				t.Fatalf("support = %v, want %s", err, test.code)
			}
		})
	}
}
