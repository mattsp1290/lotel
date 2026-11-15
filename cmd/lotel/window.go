package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var windowTimestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)

func parseWindowTimestamp(value string) (time.Time, error) {
	if !windowTimestampPattern.MatchString(value) {
		return time.Time{}, fmt.Errorf("expected RFC3339 timestamp with at most nine fractional digits")
	}
	return time.Parse(time.RFC3339Nano, value)
}

func windowSince(value string, now time.Time) (time.Time, error) {
	if t, err := parseWindowTimestamp(value); err == nil {
		return t.UTC(), nil
	}
	var duration time.Duration
	var err error
	if strings.HasSuffix(value, "d") {
		days := strings.TrimSuffix(value, "d")
		if days == "" || strings.Trim(days, "0123456789") != "" {
			return time.Time{}, fmt.Errorf("invalid duration")
		}
		var n int64
		n, err = strconv.ParseInt(days, 10, 64)
		if err == nil && n <= int64((1<<63-1)/int64(24*time.Hour)) {
			duration = time.Duration(n) * 24 * time.Hour
		} else {
			err = fmt.Errorf("invalid duration")
		}
	} else {
		duration, err = time.ParseDuration(value)
	}
	if err != nil || duration <= 0 {
		return time.Time{}, fmt.Errorf("expected an RFC3339 timestamp or positive duration")
	}
	return now.Add(-duration), nil
}

func resolveBoundedWindow(since, until string, untilSet bool, now time.Time) (time.Time, time.Time, error) {
	start, err := windowSince(since, now)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --since: %w", err)
	}
	end := now
	if untilSet {
		end, err = parseWindowTimestamp(until)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --until: %w", err)
		}
		end = end.UTC()
	}
	return start, end, nil
}

func validBoundedWindow(start, end, now time.Time) bool {
	return !start.IsZero() && !end.IsZero() && !end.After(now) && !start.After(end)
}
