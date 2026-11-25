package telegram

import (
	"time"
)

type duplicateCandidate struct {
	ID, Amount, Merchant string
	At                   time.Time
}
