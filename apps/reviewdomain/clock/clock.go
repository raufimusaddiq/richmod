// Package clock defines the household operating timezone used by all finance logic.
package clock

import (
	"time"
	_ "time/tzdata"
)

const HouseholdTimezone = "Asia/Jakarta"

func HouseholdLocation() *time.Location {
	return jakarta
}

var jakarta = func() *time.Location {
	location, err := time.LoadLocation(HouseholdTimezone)
	if err != nil {
		panic(err)
	}
	return location
}()
