package storage

import "time"

func projectedWindow(since, until time.Time) (time.Time, time.Time) {
	start := since.Truncate(time.Microsecond)
	if start.Before(since) {
		start = start.Add(time.Microsecond)
	}
	return start, until.Truncate(time.Microsecond)
}
