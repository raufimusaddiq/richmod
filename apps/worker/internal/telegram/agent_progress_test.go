package telegram

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProgressNoticeIsSentOnceIfTheTurnIsStillRunning(t *testing.T) {
	var sent atomic.Int32
	stop := startProgressNotice(20*time.Millisecond, func(context.Context) error { sent.Add(1); return nil })
	time.Sleep(120 * time.Millisecond)
	stop()
	if got := sent.Load(); got != 1 {
		t.Fatalf("a long turn must send exactly one notice, got %d", got)
	}
	time.Sleep(60 * time.Millisecond)
	if got := sent.Load(); got != 1 {
		t.Fatalf("the notice must never repeat, got %d", got)
	}
}

func TestProgressNoticeIsNotSentWhenTheTurnFinishesFirst(t *testing.T) {
	var sent atomic.Int32
	stop := startProgressNotice(80*time.Millisecond, func(context.Context) error { sent.Add(1); return nil })
	stop() // the answer arrived before the delay
	time.Sleep(200 * time.Millisecond)
	if got := sent.Load(); got != 0 {
		t.Fatalf("a fast turn must stay silent, got %d notices", got)
	}
}

func TestProgressNoticeNeverFollowsTheAnswer(t *testing.T) {
	var sent atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	stop := startProgressNotice(10*time.Millisecond, func(context.Context) error {
		close(started)
		<-release // a slow insert that is still in flight when the turn ends
		sent.Add(1)
		return nil
	})
	<-started
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
		t.Fatal("stop must wait for an in-flight notice so it cannot land after the answer")
	case <-time.After(60 * time.Millisecond):
	}
	close(release)
	<-done
	if got := sent.Load(); got != 1 {
		t.Fatalf("the in-flight notice completes before stop returns, got %d", got)
	}
}

func TestProgressNoticeSurvivesASendFailure(t *testing.T) {
	stop := startProgressNotice(10*time.Millisecond, func(context.Context) error { return context.DeadlineExceeded })
	time.Sleep(60 * time.Millisecond)
	stop() // a failed notice must not panic or block the turn
}

func TestProgressNoticeCopyIsPlainAndPointsAtTheAnswer(t *testing.T) {
	if !strings.Contains(progressNoticeMessage, "menyusul") || len([]rune(progressNoticeMessage)) > 200 {
		t.Fatalf("the notice must say the answer is coming, briefly: %q", progressNoticeMessage)
	}
}
