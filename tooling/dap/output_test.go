package dap

import "testing"

func TestOutputBufferReadsEachHostStreamIncrementally(t *testing.T) {
	output := NewOutputBuffer()
	if _, err := output.StdoutWriter().Write([]byte("out")); err != nil {
		t.Fatal(err)
	}
	if _, err := output.StderrWriter().Write([]byte("err")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, stdoutOffset, stderrOffset, err := output.ReadOutput(0, 0)
	if err != nil || stdout != "out" || stderr != "err" {
		t.Fatalf("first read = %q, %q, %v", stdout, stderr, err)
	}
	if _, err := output.StdoutWriter().Write([]byte("put")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, _, _, err = output.ReadOutput(stdoutOffset, stderrOffset)
	if err != nil || stdout != "put" || stderr != "" {
		t.Fatalf("incremental read = %q, %q, %v", stdout, stderr, err)
	}
}
