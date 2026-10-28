package accountrunner

import (
	"log/slog"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/config"
)

// fakeSender captures messages sent by the Runner.
type fakeSender struct {
	msgs chan tea.Msg
}

func newFakeSender() *fakeSender {
	return &fakeSender{msgs: make(chan tea.Msg, 64)}
}

func (f *fakeSender) Send(msg tea.Msg) {
	f.msgs <- msg
}

// testConfig returns a minimal Config with sensible intervals.
func testConfig() *config.Config {
	return &config.Config{
		General: config.General{
			PollInterval: 10 * time.Millisecond,
			SendInterval: 10 * time.Millisecond,
		},
		Accounts: []config.Account{
			{Name: "work", Email: "work@example.com"},
			{Name: "personal", Email: "personal@example.com"},
		},
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestRunner_StartStop verifies that a Runner with nil engines starts and
// stops without goroutine leaks.
func TestRunner_StartStop(t *testing.T) {
	cfg := testConfig()
	fs := newFakeSender()
	r := New(nil, nil, cfg, testLogger(), fs)

	acct := config.Account{Name: "work", Email: "work@example.com"}
	if err := r.Start(acct); err != nil {
		t.Fatalf("Start: unexpected error: %v", err)
	}

	// Give goroutines a moment to settle (they should be idle with nil engines).
	time.Sleep(5 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		if err := r.Stop(); err != nil {
			t.Errorf("Stop: unexpected error: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(time.Second):
		t.Fatal("Stop did not return within 1s — goroutine leak suspected")
	}

	if got := r.Active(); got != "" {
		t.Errorf("Active after Stop = %q, want \"\"", got)
	}
}

// TestRunner_StartIdempotent verifies that starting the same account twice
// is a no-op and returns nil.
func TestRunner_StartIdempotent(t *testing.T) {
	cfg := testConfig()
	r := New(nil, nil, cfg, testLogger(), newFakeSender())

	acct := config.Account{Name: "work", Email: "work@example.com"}

	if err := r.Start(acct); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := r.Start(acct); err != nil {
		t.Fatalf("second Start (idempotent): %v", err)
	}

	if got := r.Active(); got != "work" {
		t.Errorf("Active = %q, want \"work\"", got)
	}

	_ = r.Stop()
}

// TestRunner_Active verifies the Active transitions around Start / Stop.
func TestRunner_Active(t *testing.T) {
	cfg := testConfig()
	r := New(nil, nil, cfg, testLogger(), newFakeSender())

	if got := r.Active(); got != "" {
		t.Errorf("Active before Start = %q, want \"\"", got)
	}

	acct := config.Account{Name: "work", Email: "work@example.com"}
	if err := r.Start(acct); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := r.Active(); got != "work" {
		t.Errorf("Active after Start = %q, want \"work\"", got)
	}

	if err := r.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := r.Active(); got != "" {
		t.Errorf("Active after Stop = %q, want \"\"", got)
	}
}

// TestRunner_SwitchChangesActive verifies that Switch stops the old account
// and starts the new one.
func TestRunner_SwitchChangesActive(t *testing.T) {
	cfg := testConfig()
	r := New(nil, nil, cfg, testLogger(), newFakeSender())

	work := config.Account{Name: "work", Email: "work@example.com"}
	personal := config.Account{Name: "personal", Email: "personal@example.com"}

	if err := r.Start(work); err != nil {
		t.Fatalf("Start work: %v", err)
	}
	if err := r.Switch(personal); err != nil {
		t.Fatalf("Switch to personal: %v", err)
	}

	if got := r.Active(); got != "personal" {
		t.Errorf("Active after Switch = %q, want \"personal\"", got)
	}

	_ = r.Stop()
}

// TestRunner_StopIdempotent verifies that Stop with nothing running returns nil.
func TestRunner_StopIdempotent(t *testing.T) {
	cfg := testConfig()
	r := New(nil, nil, cfg, testLogger(), newFakeSender())

	if err := r.Stop(); err != nil {
		t.Errorf("Stop (nothing running): %v", err)
	}
	// Second call is also safe.
	if err := r.Stop(); err != nil {
		t.Errorf("Stop (second call): %v", err)
	}
}
