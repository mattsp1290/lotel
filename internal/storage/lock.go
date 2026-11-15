package storage

import "strings"

func IsDatabaseLocked(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Could not set lock")
}
