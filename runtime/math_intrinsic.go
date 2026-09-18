package runtime

import "math"

func mathFloat64bits(_ intrinsicContext, args []vmValue) ([]vmValue, error) {
	value, err := numericAsFloat64(args[0])
	if err != nil {
		return nil, err
	}
	return []vmValue{newVMValue("Uint64", math.Float64bits(value))}, nil
}

func mathFloat64frombits(_ intrinsicContext, args []vmValue) ([]vmValue, error) {
	value, err := numericAsUint64(args[0])
	if err != nil {
		return nil, err
	}
	return []vmValue{newVMValue("Float64", math.Float64frombits(value))}, nil
}

func mathFloat32bits(_ intrinsicContext, args []vmValue) ([]vmValue, error) {
	value, err := numericAsFloat64(args[0])
	if err != nil {
		return nil, err
	}
	return []vmValue{newVMValue("Uint32", uint64(math.Float32bits(float32(value))))}, nil
}

func mathFloat32frombits(_ intrinsicContext, args []vmValue) ([]vmValue, error) {
	value, err := numericAsUint64(args[0])
	if err != nil {
		return nil, err
	}
	return []vmValue{newVMValue("Float32", float64(math.Float32frombits(uint32(value))))}, nil
}
