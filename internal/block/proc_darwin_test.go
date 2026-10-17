package block

import (
	"os/exec"
	"reflect"
	"testing"
)

// procArgs reads a live process's arguments from kern.procargs2, empty
// ones included.
func TestProcArgs(t *testing.T) {
	cmd := exec.Command("sleep", "30", "")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	want := []string{"sleep", "30", ""}
	var got []string
	waitFor(t, "arguments readable", func() bool {
		got = procArgs(cmd.Process.Pid)
		return reflect.DeepEqual(got, want)
	})
}
