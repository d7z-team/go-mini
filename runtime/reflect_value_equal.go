package runtime

import (
	"errors"
	"fmt"
)

func reflectValueEqual(ctx intrinsicContext, args []vmValue) ([]vmValue, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("reflect.value_equal expects 2 arguments, got %d", len(args))
	}
	leftFields, err := reflectValuePayload(args[0])
	if err != nil {
		return reflectEqualError(err.Error()), nil
	}
	rightFields, err := reflectValuePayload(args[1])
	if err != nil {
		return reflectEqualError(err.Error()), nil
	}
	leftValid := reflectBoolField(leftFields.get("valid"))
	rightValid := reflectBoolField(rightFields.get("valid"))
	if !leftValid || !rightValid {
		return reflectEqualResult(leftValid == rightValid), nil
	}
	left, leftOK, err := reflectCurrentValue(leftFields)
	if err != nil || !leftOK {
		if err == nil {
			err = errors.New("reflect.Value.Equal: invalid left Value")
		}
		return reflectEqualError(err.Error()), nil
	}
	right, rightOK, err := reflectCurrentValue(rightFields)
	if err != nil || !rightOK {
		if err == nil {
			err = errors.New("reflect.Value.Equal: invalid right Value")
		}
		return reflectEqualError(err.Error()), nil
	}
	module := reflectRelationModule(ctx)
	if module == nil {
		return reflectEqualError("reflect.Value.Equal requires module context"), nil
	}
	left, leftOK, err = reflectDeepDynamic(module, left)
	if err != nil {
		return reflectEqualError("reflect.Value.Equal: " + err.Error()), nil
	}
	right, rightOK, err = reflectDeepDynamic(module, right)
	if err != nil {
		return reflectEqualError("reflect.Value.Equal: " + err.Error()), nil
	}
	if !leftOK || !rightOK {
		return reflectEqualResult(leftOK == rightOK), nil
	}
	if !module.sameRuntimeType(left.Type, right.Type) {
		return reflectEqualResult(false), nil
	}
	equal, err := module.equalValues(left, right)
	if err != nil {
		return reflectEqualError("reflect.Value.Equal: " + err.Error()), nil
	}
	return reflectEqualResult(equal), nil
}

func reflectEqualResult(equal bool) []vmValue {
	return []vmValue{newVMValue("Bool", equal), newVMValue("String", ""), newVMValue("Bool", true)}
}

func reflectEqualError(message string) []vmValue {
	return []vmValue{newVMValue("Bool", false), newVMValue("String", message), newVMValue("Bool", false)}
}
