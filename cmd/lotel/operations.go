package main

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mattsp1290/lotel/internal/config"
	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

func ingestCommand() *cobra.Command {
	var fullIngest bool
	cmd := &cobra.Command{
		Use: "ingest", Short: "Ingest JSONL telemetry files into the query database",
		RunE: withDatabaseJSON(func(cmd *cobra.Command, args []string, db *sql.DB) (any, error) {
			dataPath, err := config.DataPath()
			if err != nil {
				return nil, err
			}
			if fullIngest {
				return storage.ReingestAll(db, dataPath)
			}
			return storage.IngestAll(db, dataPath)
		}, true),
	}
	cmd.Flags().BoolVar(&fullIngest, "full", false, "rebuild the query index from retained JSONL; previously pruned records can return")
	return cmd
}

func pruneCommand() *cobra.Command {
	var olderThan, service string
	var dryRun, all bool
	cmd := &cobra.Command{
		Use: "prune", Short: "Delete telemetry data older than a threshold",
		RunE: func(cmd *cobra.Command, args []string) error {
			var reports []storage.PruneReport
			err := withDatabase(func(cmd *cobra.Command, args []string, db *sql.DB) error {
				if all && olderThan != "" {
					return fmt.Errorf("--all and --older-than are mutually exclusive")
				}
				if !all && olderThan == "" {
					return fmt.Errorf("--older-than or --all is required (e.g., '7d', '24h')")
				}
				var cutoff time.Time
				if all {
					cutoff = time.Now()
				} else {
					dur, err := parseDuration(olderThan)
					if err != nil {
						return fmt.Errorf("invalid --older-than: %w", err)
					}
					cutoff = time.Now().Add(-dur)
				}
				prune := storage.Prune
				if all {
					prune = storage.PruneAll
				}
				var err error
				reports, err = prune(db, cutoff, service, dryRun)
				return err
			})(cmd, args)
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Fprintln(cmd.ErrOrStderr(), "Dry run — no data was deleted.")
			}
			return writeJSON(cmd, reports, true)
		},
	}
	cmd.Flags().StringVar(&olderThan, "older-than", "", "age threshold (e.g., '7d', '24h', '1h')")
	cmd.Flags().StringVar(&service, "service", "", "limit pruning to a specific service")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be pruned without deleting")
	cmd.Flags().BoolVar(&all, "all", false, "delete all telemetry data")
	return cmd
}
