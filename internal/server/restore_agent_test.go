package server

import (
	"reflect"
	"testing"

	"github.com/manueltarouca/hive/internal/block"
	"github.com/manueltarouca/hive/internal/persist"
)

// An agent is resumed after a restart only if it was still running: one
// killed without its SessionEnd hook leaves the pane at a shell prompt,
// and resuming would reopen a session the user closed.
func TestRestoreResumesOnlyRunningAgents(t *testing.T) {
	resume := func(h, id string) []string { return []string{h, "--resume", id} }
	agent := &persist.Agent{Harness: "claude", SessionID: "s1", State: "idle"}
	cases := []struct {
		name string
		fg   *block.Process
		want []string
	}{
		{"agent in front", &block.Process{Name: "2.1.302", Program: "claude"}, []string{"claude", "--resume", "s1"}},
		{"older state, no program", &block.Process{Name: "2.1.302"}, []string{"claude", "--resume", "s1"}},
		{"nothing recorded", nil, []string{"claude", "--resume", "s1"}},
		{"back at zsh", &block.Process{Name: "zsh", Program: "zsh"}, nil},
		{"older state at a login shell", &block.Process{Name: "-bash"}, nil},
	}
	for _, c := range cases {
		st := &persist.BlockState{Agent: agent, Foreground: c.fg}
		got, _ := restoreCommand(st, resume)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: argv = %v, want %v", c.name, got, c.want)
		}
	}
}
