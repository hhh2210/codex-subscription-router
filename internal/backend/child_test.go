package backend

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"testing"
	"time"

	"github.com/b-nnett/codex-subscription-router/internal/protocol"
)

func TestStopWaitsForInterruptIgnoringChildToExit(t *testing.T) {
	inbound := make(chan Inbound, 1)
	child, err := Start("test", t.TempDir(), os.Args[0], []string{"-test.run=TestInterruptIgnoringHelper"},
		append(os.Environ(), "CODEX_MUX_STOP_HELPER=1"), inbound)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ctx, cancel := context.WithCancel(context.Background()); cancel(); _ = child.Stop(ctx) })
	select {
	case <-inbound: // The signal handler is installed before readiness.
	case <-time.After(5 * time.Second):
		t.Fatal("child did not become ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := child.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-child.closed:
	default:
		t.Fatal("Stop returned before process exit")
	}
	if child.command.ProcessState == nil {
		t.Fatal("process was not reaped")
	}
	if err := child.Send(protocol.Message{Method: "test"}); err == nil {
		t.Fatal("stopped process accepted a request")
	}
}

func TestInterruptIgnoringHelper(t *testing.T) {
	if os.Getenv("CODEX_MUX_STOP_HELPER") != "1" {
		return
	}
	signal.Ignore(os.Interrupt)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"method": "ready"})
	for {
		time.Sleep(time.Hour)
	}
}
