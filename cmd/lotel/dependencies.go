package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

func dependenciesCommand() *cobra.Command {
	var since, until string
	cmd := &cobra.Command{
		Use: "dependencies", Short: "Summarize observed service dependencies (JSON output)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now().UTC()
			start, end, err := resolveBoundedWindow(since, until, cmd.Flags().Changed("until"), now)
			if err != nil {
				return err
			}
			if !validBoundedWindow(start, end, now) {
				return fmt.Errorf("invalid dependency window")
			}
			return withDatabase(func(cmd *cobra.Command, args []string, db *sql.DB) error {
				report, err := storage.QueryDependencies(db, storage.DependencyOptions{Since: start, Until: end})
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			})(cmd, args)
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "inclusive start (RFC3339 or positive duration, required)")
	cmd.Flags().StringVar(&until, "until", "", "inclusive end (RFC3339, default now)")
	return cmd
}
