package main

import (
	"errors"
	"fmt"

	runtimepkg "github.com/d7z-team/mini-go/runtime"
)

func parseTestReport(result runtimepkg.RunResult) (bool, []string, error) {
	if len(result.Values) != 1 {
		return false, nil, fmt.Errorf("test entry returned %d values", len(result.Values))
	}
	fields, ok := result.Values[0].Fields()
	if !ok {
		return false, nil, errors.New("test entry did not return testing.Report")
	}
	passed := false
	foundPassed := false
	var failures []string
	for _, field := range fields {
		switch field.Name {
		case "Passed":
			value, ok := field.Value.Bool()
			if !ok {
				return false, nil, errors.New("testing.Report.Passed is not Bool")
			}
			passed = value
			foundPassed = true
		case "Results":
			results, ok := field.Value.Items()
			if !ok && field.Value.Kind() != runtimepkg.HostNilKind {
				return false, nil, errors.New("testing.Report.Results is not a slice")
			}
			for _, item := range results {
				resultFields, ok := item.Fields()
				if !ok {
					return false, nil, errors.New("testing.Report.Results contains a non-struct value")
				}
				name, status, message := "", "", ""
				for _, resultField := range resultFields {
					value, _ := resultField.Value.StringValue()
					switch resultField.Name {
					case "Name":
						name = value
					case "Status":
						status = value
					case "Message":
						message = value
					}
				}
				if status == "fail" {
					if message != "" {
						name += ": " + message
					}
					failures = append(failures, name)
				}
			}
		}
	}
	if !foundPassed {
		return false, nil, errors.New("testing.Report has no Passed field")
	}
	return passed, failures, nil
}
