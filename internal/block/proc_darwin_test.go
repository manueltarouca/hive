package block

import (
	"os"
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

// A hook finds the agent it runs for past the shells between them.
func TestAgentProcessSkipsShells(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30; :") // sh stays, sleep is its child
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if got := Parent(cmd.Process.Pid); got != os.Getpid() {
		t.Fatalf("Parent(sh) = %d, want %d", got, os.Getpid())
	}
	if got := AgentProcess(cmd.Process.Pid); got != os.Getpid() {
		t.Fatalf("AgentProcess(sh) = %d, want this test (%d)", got, os.Getpid())
	}
}
