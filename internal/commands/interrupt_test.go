//go:build unix

package commands

import (
	"syscall"
	"testing"
	"time"
)

func TestCtrlCAtThePromptRunsTheRestoreInsteadOfKillingCw(t *testing.T) {
	restored := make(chan struct{})
	stop := onInterrupt(func() { close(restored) })
	defer stop()
	// Without the handler this SIGINT would end the test binary.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-restored:
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl-C did not reach the handler")
	}
}

func TestAfterThePromptCtrlCIsNoLongerCaught(t *testing.T) {
	called := make(chan struct{}, 1)
	stop := onInterrupt(func() { called <- struct{}{} })
	stop()
	// Another handler stands in for the default one, so the test binary lives.
	caught := make(chan struct{})
	other := onInterrupt(func() { close(caught) })
	defer other()
	syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	select {
	case <-caught:
	case <-time.After(5 * time.Second):
		t.Fatal("no handler saw the signal")
	}
	select {
	case <-called:
		t.Error("a stopped handler still ran")
	case <-time.After(100 * time.Millisecond):
	}
}
