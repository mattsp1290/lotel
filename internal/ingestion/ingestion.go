package ingestion

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

type Options struct {
	Enabled  bool
	Interval time.Duration
}

func Run(ctx context.Context, dataPath string, options Options, reportError func(error)) error {
	if !options.Enabled {
		return nil
	}
	if options.Interval <= 0 {
		return errors.New("ingestion interval must be positive")
	}
	ticker := time.NewTicker(options.Interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := ingest(dataPath); err != nil && reportError != nil {
			reportError(err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func ingest(dataPath string) error {
	db, err := storage.OpenDB(filepath.Join(dataPath, "lotel.db"))
	if err != nil {
		return fmt.Errorf("opening ingestion database: %w", err)
	}
	_, ingestErr := storage.IngestAll(db, dataPath)
	return errors.Join(ingestErr, db.Close())
}
