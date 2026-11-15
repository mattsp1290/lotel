package storage

import (
	"strings"
	"testing"
	"time"
)

func TestRecentLogsRejectsUnsupportedNameFilter(t *testing.T) {
	now := time.Now()
	_, _, err := RecentLogs(nil, LogQueryOptions{QueryOptions: QueryOptions{
		Name: "unavailable", Since: now.Add(-time.Minute), Until: now, Limit: 1,
	}})
	if err == nil || !strings.Contains(err.Error(), "name filter") {
		t.Fatalf("unsupported filter must fail before querying: %v", err)
	}
}
