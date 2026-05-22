package synclib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/synclib"
)

func TestWaitGroup(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("sync", synclib.Module_FFI_Schemas),
		testutil.FFISchema("sync.WaitGroup", synclib.WaitGroup_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "synchronizes-execution-contexts",
			Imports: []string{"sync", "time"},
			Decls: `
func worker(wg *sync.WaitGroup, dst []int, idx int, value int) {
	dst[idx] = value
	wg.Done()
}
`,
			Body: `
wg := sync.NewWaitGroup()
values := []int{0, 0, 0}
wg.Add(3)

go worker(wg, values, 0, 11)
go worker(wg, values, 1, 22)
go worker(wg, values, 2, 33)

wg.Wait()
sum := values[0] + values[1] + values[2]

gate := sync.NewWaitGroup()
ready := sync.NewWaitGroup()
released := sync.NewWaitGroup()
observed := 0

gate.Add(1)
ready.Add(2)
released.Add(2)

go func() {
	ready.Done()
	gate.Wait()
	observed = observed + 1
	released.Done()
}()
go func() {
	ready.Done()
	gate.Wait()
	observed = observed + 10
	released.Done()
}()

ready.Wait()
gate.Done()
released.Wait()

wg.Add(1)
go func() {
	time.Sleep(1000000)
	values[0] = 100
	wg.Done()
}()
wg.Wait()

test.OutInt(sum)
test.Out("|")
test.OutInt(observed)
test.Out("|")
test.OutInt(values[0])
`,
			Want:   "66|11|100",
			Covers: []string{"NewWaitGroup", "Add", "Done", "Wait"},
		},
		{
			Name:       "negative-counter-panics",
			Imports:    []string{"sync"},
			Body:       "wg := sync.NewWaitGroup()\nwg.Done()\n",
			WantRunErr: "negative WaitGroup counter",
			Covers:     []string{"NewWaitGroup", "Done"},
		},
	}, testutil.WithRegister(ffilib.RegisterAll))
}
