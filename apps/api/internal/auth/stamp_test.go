package auth

import (
	"sync/atomic"
	"time"
)

var lastStamp atomic.Int64

func uniqueStamp() int64 {
	for {
		last := lastStamp.Load()
		next := max(time.Now().UnixNano(), last+1_000_000)
		if lastStamp.CompareAndSwap(last, next) {
			return next
		}
	}
}
