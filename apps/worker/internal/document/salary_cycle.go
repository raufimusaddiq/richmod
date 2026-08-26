package document

import "time"

// SalaryCycle returns the half-open cycle containing at for confirmed anchor
// pay dates. The current cycle remains open when no later anchor exists.
func SalaryCycle(anchorDates []time.Time, at time.Time) (time.Time, *time.Time, bool) {
	if len(anchorDates) == 0 {
		return time.Time{}, nil, false
	}
	start := time.Time{}
	for _, d := range anchorDates {
		if d.After(at) {
			break
		}
		if start.IsZero() || d.After(start) {
			start = d
		}
	}
	if start.IsZero() {
		return time.Time{}, nil, false
	}
	for _, d := range anchorDates {
		if d.After(start) && (at.Before(d) || at.Equal(d)) {
			end := d
			return start, &end, true
		}
	}
	return start, nil, true
}
