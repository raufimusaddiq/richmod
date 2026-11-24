package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/queue"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

type settlement int

const (
	settleFail settlement = iota
	settleDefer
	settleRelease
)

const (
	maxModelJobDeferrals = 30
	laneShutdownTimeout  = 30 * time.Second
)

var modelJobTypes = map[string]bool{
	"PROCESS_TELEGRAM_TEXT":           true,
	"PROCESS_BANK_EMAIL":              true,
	"PROCESS_FINANCIAL_EMAIL":         true,
	"PROCESS_FINANCIAL_EMAIL_PREVIEW": true,
	"PROCESS_DOCUMENT":                true,
	"PROCESS_PAYSLIP":                 true,
	"PROCESS_RECEIPT":                 true,
	"PROCESS_TRANSACTION_SCREENSHOT":  true,
	"GENERATE_INSIGHT":                true,
}

var interactiveModelJobTypes = map[string]bool{
	"PROCESS_FINANCIAL_EMAIL_PREVIEW": true,
}

func settle(workerStopping bool, job queue.Job, err error) (settlement, error) {
	if workerStopping && errors.Is(err, context.Canceled) {
		return settleRelease, err
	}
	if job.Type == "PROCESS_TELEGRAM_TEXT" {
		if telegram.IsGatewayDeferral(err) {
			return settleDefer, err
		}
		return settleFail, telegram.TextFailurePolicy(err, job.Attempts)
	}
	if !modelJobTypes[job.Type] {
		return settleFail, err
	}
	if gateway.IsUnavailable(err) && !interactiveModelJobTypes[job.Type] && job.Deferrals < maxModelJobDeferrals {
		return settleDefer, err
	}
	if gateway.IsRejected(err) {
		return settleFail, queue.Permanent(err)
	}
	return settleFail, err
}

func waitForLanes(group *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
