package stdlib_test

import (
	"context"
	"testing"

	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func FuzzFloatRoundTrip(f *testing.F) {
	for _, bits := range []uint64{0, 1, 1 << 63, 0x0010000000000000, 0x3fb999999999999a, 0x7fefffffffffffff, 0x7ff0000000000000, 0x7ff8000000000000} {
		f.Add(bits, false)
		f.Add(bits, true)
	}
	program := prepareStdlibRunProgram(f, "minigo.test/float-roundtrip", `package main
import ("math"; "strconv")
func Run(bits uint64, single bool) bool {
	value := math.Float64frombits(bits)
	width := 64
	if single { value = float64(float32(value)); width = 32 }
	if math.IsNaN(value) { return true }
	text := strconv.FormatFloat(value, 'g', -1, width)
	parsed, err := strconv.ParseFloat(text, width)
	return err == nil && math.Float64bits(value) == math.Float64bits(parsed)
}`)
	f.Fuzz(func(t *testing.T, bits uint64, single bool) {
		instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		result, err := instance.Call(context.Background(), "run", minigoruntime.HostUint("Uint64", bits), minigoruntime.HostBool(single))
		if err != nil {
			t.Fatal(err)
		}
		if valid, ok := result.Values[0].Bool(); !ok || !valid {
			t.Fatalf("roundtrip changed bits %x (float32=%t)", bits, single)
		}
	})
}
