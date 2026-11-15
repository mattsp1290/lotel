package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

type Options struct {
	Enabled  bool
	Interval time.Duration
}
type Status struct {
	Enabled     bool       `json:"enabled"`
	Running     bool       `json:"running"`
	Interval    string     `json:"interval"`
	LastSuccess *time.Time `json:"last_success,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}
type Worker struct {
	options Options
	mu      sync.RWMutex
	status  Status
}

func New(options Options) *Worker {
	return &Worker{options: options, status: Status{Enabled: options.Enabled, Interval: options.Interval.String()}}
}
func (w *Worker) Status() Status { w.mu.RLock(); defer w.mu.RUnlock(); return w.status }
func (w *Worker) Run(ctx context.Context, dataPath string, reportError func(error)) error {
	if !w.options.Enabled {
		return nil
	}
	if w.options.Interval <= 0 {
		return errors.New("ingestion interval must be positive")
	}
	w.mu.Lock()
	w.status.Running = true
	w.mu.Unlock()
	defer func() { w.mu.Lock(); w.status.Running = false; w.mu.Unlock() }()
	ticker := time.NewTicker(w.options.Interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		passCtx, cancel := context.WithTimeout(ctx, w.options.Interval)
		err := Ingest(passCtx, dataPath)
		cancel()
		w.mu.Lock()
		if err == nil {
			now := time.Now().UTC()
			w.status.LastSuccess = &now
			w.status.LastError = ""
		} else if ctx.Err() == nil {
			w.status.LastError = err.Error()
		}
		w.mu.Unlock()
		if err != nil && ctx.Err() == nil && reportError != nil {
			reportError(err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
func Ingest(ctx context.Context, dataPath string) error {
	return storage.WithDB(ctx, filepath.Join(dataPath, "lotel.db"), func(db *sql.DB) error { _, err := storage.IngestAll(db, dataPath); return err })
}
