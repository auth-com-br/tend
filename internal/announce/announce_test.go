package announce

import (
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// TestOnlyStoppingIsWorthSaying covers herdr's rule and the cooldown tend
// adds: an agent flickers between working and idle while a tool runs, and a
// notification a second is worse than none.
func TestOnlyStoppingIsWorthSaying(t *testing.T) {
	t0 := time.Now()
	for _, c := range []struct {
		name     string
		previous Notice
		state    detect.State
		at       time.Time
		kind     Kind
		want     bool
	}{
		{"blocked from working", Notice{State: detect.StateWorking, At: t0}, detect.StateBlocked, t0, Blocked, true},
		{"finished", Notice{State: detect.StateWorking, At: t0}, detect.StateIdle, t0, Finished, true},
		{"idle that was never working", Notice{State: detect.StateIdle, At: t0}, detect.StateIdle, t0, Nothing, false},
		{"still working", Notice{State: detect.StateWorking, At: t0}, detect.StateWorking, t0, Nothing, false},
		{"blocked again, too soon", Notice{State: detect.StateBlocked, At: t0}, detect.StateBlocked, t0.Add(time.Second), Nothing, false},
		{"blocked again, much later", Notice{State: detect.StateWorking, At: t0}, detect.StateBlocked, t0.Add(time.Hour), Blocked, true},
	} {
		kind, ok := Worth(c.previous, c.state, c.at)
		if ok != c.want || (c.want && kind != c.kind) {
			t.Errorf("%s: Worth = %d, %v; want %d, %v", c.name, kind, ok, c.kind, c.want)
		}
	}
}

// TestHeldNewsIsSaidOnlyWhileTrue: herdr's notification_validation. An
// agent answered in the meantime is not "needing attention" any more, and
// one that has not been read yet is waited for, not forgotten.
func TestHeldNewsIsSaidOnlyWhileTrue(t *testing.T) {
	snap := &proto.SessionSnapshot{Panes: []proto.PaneInfo{
		{ID: 1, State: "blocked"},
		{ID: 2, State: "idle", Done: true},
		{ID: 3, State: "working"},
		{ID: 4, State: "idle"},
	}}
	for _, c := range []struct {
		name string
		kind string
		pane uint64
		snap *proto.SessionSnapshot
		want Check
	}{
		{"still blocked", ui.ToastAttention, 1, snap, Current},
		{"answered since", ui.ToastAttention, 4, snap, Stale},
		{"done", ui.ToastFinished, 2, snap, Current},
		{"still working, wait", ui.ToastFinished, 3, snap, Awaiting},
		{"not read yet", ui.ToastAttention, 1, nil, Awaiting},
		{"a pane not in the snapshot yet", ui.ToastAttention, 9, snap, Awaiting},
		{"a script's own", ui.ToastCustom, 0, nil, Current},
	} {
		if got := Validate(c.kind, c.pane, c.snap); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}
