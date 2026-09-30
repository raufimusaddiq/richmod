// Package clock preserves the API timezone interface over the shared clock.
package clock

import (
	"time"

	shared "github.com/raufimusaddiq/richmod/apps/reviewdomain/clock"
)

const HouseholdTimezone = shared.HouseholdTimezone

func HouseholdLocation() *time.Location { return shared.HouseholdLocation() }
