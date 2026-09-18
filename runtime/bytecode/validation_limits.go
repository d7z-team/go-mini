package bytecode

import "fmt"

type ValidationLimits struct {
	MaxTypes               int
	MaxConstants           int
	MaxGlobals             int
	MaxFunctions           int
	MaxExports             int
	MaxRequirements        int
	MaxInstructions        int
	MaxLocalsPerFunction   int
	MaxUpvaluesPerFunction int
	MaxPayloadBytes        int
	MaxConstantBytes       int
}

func DefaultValidationLimits() ValidationLimits {
	return defaultValidationLimits
}

func validateArtifactLimits(a *Artifact, limits ValidationLimits) error {
	if err := validateCountLimit("types", len(a.TypeTable.Nodes), limits.MaxTypes); err != nil {
		return err
	}
	if err := validateCountLimit("constants", len(a.Constants), limits.MaxConstants); err != nil {
		return err
	}
	if err := validateCountLimit("globals", len(a.Globals), limits.MaxGlobals); err != nil {
		return err
	}
	if err := validateCountLimit("functions", len(a.Functions), limits.MaxFunctions); err != nil {
		return err
	}
	if err := validateCountLimit("exports", len(a.Exports), limits.MaxExports); err != nil {
		return err
	}
	if err := validateCountLimit("requirements", len(a.Requirements), limits.MaxRequirements); err != nil {
		return err
	}
	totalInstructions := 0
	totalPayloadBytes := 0
	for i, fn := range a.Functions {
		path := fmt.Sprintf("functions[%d]", i)
		if err := validateCountLimit(path+".locals", len(fn.Locals), limits.MaxLocalsPerFunction); err != nil {
			return err
		}
		if err := validateCountLimit(path+".upvalues", len(fn.Upvalues), limits.MaxUpvaluesPerFunction); err != nil {
			return err
		}
		totalInstructions += len(fn.Instructions)
		for j, inst := range fn.Instructions {
			totalPayloadBytes += len(inst.Payload)
			if err := validateCountLimit(fmt.Sprintf("%s.instructions[%d].payload_bytes", path, j), len(inst.Payload), limits.MaxPayloadBytes); err != nil {
				return err
			}
		}
	}
	if err := validateCountLimit("instructions", totalInstructions, limits.MaxInstructions); err != nil {
		return err
	}
	if err := validateCountLimit("instruction_payload_bytes", totalPayloadBytes, limits.MaxPayloadBytes); err != nil {
		return err
	}
	totalConstantBytes := 0
	for i, constant := range a.Constants {
		totalConstantBytes += len(constant.Value)
		if err := validateCountLimit(fmt.Sprintf("constants[%d].value_bytes", i), len(constant.Value), limits.MaxConstantBytes); err != nil {
			return err
		}
	}
	return validateCountLimit("constant_value_bytes", totalConstantBytes, limits.MaxConstantBytes)
}

func validateCountLimit(path string, got, limit int) error {
	if limit <= 0 || got <= limit {
		return nil
	}
	return newCodedValidationError(ValidationLimitExceeded, path, fmt.Errorf("limit exceeded: got %d, max %d", got, limit))
}
