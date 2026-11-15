package collector

import (
	"context"
	"fmt"

	"github.com/mattsp1290/lotel/internal/ingestion"
)

type runtimeShutdownResult struct {
	Error string `json:"error,omitempty"`
}

func ingestFinal(ctx context.Context, dataPath string) error {
	if err := ingestion.Ingest(ctx, dataPath); err != nil {
		return fmt.Errorf("final ingestion: %w", err)
	}
	return nil
}
