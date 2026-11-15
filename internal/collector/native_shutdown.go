package collector

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

type nativeShutdownResult struct {
	Error string `json:"error,omitempty"`
}

func ingestFinal(ctx context.Context, dataPath string) error {
	for {
		err := ingestFinalAttempt(dataPath)
		if err == nil {
			return nil
		}
		if !storage.IsDatabaseLocked(err) {
			return fmt.Errorf("final ingestion: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("final ingestion: %w", errors.Join(ctx.Err(), err))
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func ingestFinalAttempt(dataPath string) error {
	db, err := storage.OpenDB(filepath.Join(dataPath, "lotel.db"))
	if err != nil {
		return err
	}
	_, err = storage.IngestAll(db, dataPath)
	return errors.Join(err, db.Close())
}
