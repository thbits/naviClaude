package app

import (
	"errors"
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

func TestDropKillingHidesInFlightKills(t *testing.T) {
	m := Model{killing: map[string]bool{"a:1.0": true}}
	active := []*session.Session{{TmuxTarget: "a:1.0"}, {TmuxTarget: "b:1.0"}}
	got := m.dropKilling(active)
	if len(got) != 1 || got[0].TmuxTarget != "b:1.0" {
		t.Fatalf("dropKilling = %v, want only b:1.0", got)
	}
}

func TestKillDoneClearsMarkEvenOnError(t *testing.T) {
	m := Model{killing: map[string]bool{"a:1.0": true}}
	next, _ := m.Update(killDoneMsg{target: "a:1.0", err: errors.New("boom")})
	nm := next.(Model)
	if nm.killing["a:1.0"] {
		t.Error("failed kill left the session hidden")
	}
	if nm.err == nil {
		t.Error("kill error was not surfaced")
	}
}
