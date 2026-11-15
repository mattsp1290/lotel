package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func WithDB(ctx context.Context, path string, use func(*sql.DB) error) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	for {
		acquired, lockErr := tryDatabaseLock(lock)
		if lockErr != nil {
			return lockErr
		}
		if acquired {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for telemetry database: %w", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer releaseDatabaseLock(lock)
	var db *sql.DB
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		db, err = OpenDB(path)
		if err == nil {
			break
		}
		if !IsDatabaseLocked(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for telemetry database: %w", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	return use(db)
}
