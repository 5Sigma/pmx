package searchdrawer

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/mail"
)

func TestMain(m *testing.M) {
	// Shrink the debounce so Update() tests don't need to sleep for 150ms.
	debounceInterval = time.Millisecond
	m.Run()
}

func stubRow(_ *mail.Message, _ int, _ bool) string { return "" }

func newForTest() *Model {
	d := New(stubRow, func(msg *mail.Message) tea.Msg { return msg })
	d.SetSize(80, 30)
	d.Open()
	return d
}

func TestKeystrokeBumpsPendingGen(t *testing.T) {
	d := newForTest()
	before := d.PendingGen()
	cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatalf("expected tea.Cmd (tick) on keystroke, got nil")
	}
	if d.PendingGen() != before+1 {
		t.Errorf("pendingGen = %d, want %d", d.PendingGen(), before+1)
	}
}

func TestSetResultsStaleIsIgnored(t *testing.T) {
	d := newForTest()
	msg := &mail.Message{ID: 1, Subject: "hello"}
	d.SetResults(5, []*mail.Message{msg})
	if got := len(d.results); got != 1 {
		t.Fatalf("after fresh SetResults, len = %d, want 1", got)
	}
	// A stale (earlier) gen must not overwrite.
	d.SetResults(2, nil)
	if got := len(d.results); got != 1 {
		t.Errorf("after stale SetResults, len = %d, want 1 (kept)", got)
	}
}

func TestSetResultsCurrentReplaces(t *testing.T) {
	d := newForTest()
	first := []*mail.Message{{ID: 1}}
	d.SetResults(1, first)
	second := []*mail.Message{{ID: 2}, {ID: 3}}
	d.SetResults(2, second)
	if got := len(d.results); got != 2 {
		t.Errorf("len = %d, want 2", got)
	}
	if d.results[0].ID != 2 {
		t.Errorf("results[0].ID = %d, want 2", d.results[0].ID)
	}
}

func TestEnterInvokesOnSelect(t *testing.T) {
	var picked *mail.Message
	d := New(stubRow, func(msg *mail.Message) tea.Msg {
		picked = msg
		return msg
	})
	d.SetSize(80, 30)
	d.Open()
	target := &mail.Message{ID: 42}
	d.SetResults(1, []*mail.Message{target})
	cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from enter")
	}
	// The onSelect runs inside the returned cmd — invoke it synchronously.
	_ = cmd()
	if picked == nil || picked.ID != 42 {
		t.Errorf("onSelect not invoked with target message; got %+v", picked)
	}
	if d.IsOpen() {
		t.Error("drawer should be closed after enter")
	}
}

func TestEscCloses(t *testing.T) {
	d := newForTest()
	d.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if d.IsOpen() {
		t.Error("drawer should be closed after esc")
	}
}

func TestNavigationClampsCursor(t *testing.T) {
	d := newForTest()
	d.SetResults(1, []*mail.Message{{ID: 1}, {ID: 2}, {ID: 3}})
	// Down twice should land on index 2; third down should stay there.
	d.Update(tea.KeyMsg{Type: tea.KeyDown})
	d.Update(tea.KeyMsg{Type: tea.KeyDown})
	d.Update(tea.KeyMsg{Type: tea.KeyDown})
	if d.cursor != 2 {
		t.Errorf("cursor = %d, want 2", d.cursor)
	}
	// Up past top clamps to 0.
	for i := 0; i < 5; i++ {
		d.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	if d.cursor != 0 {
		t.Errorf("cursor = %d, want 0", d.cursor)
	}
}
