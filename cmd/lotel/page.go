package main

import (
	"database/sql"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

func pageCommand() *cobra.Command {
	parent := &cobra.Command{Use: "page", Short: "Page through a bounded telemetry snapshot"}
	for _, signal := range []string{"traces", "logs"} {
		var service, since, until, cursor string
		var limit int
		cmd := &cobra.Command{
			Use: signal, Args: cobra.NoArgs, Short: "Page through " + signal,
			RunE: func(cmd *cobra.Command, args []string) error {
				req := storage.PageRequest{Signal: signal, Cursor: cursor}
				if cmd.Flags().Changed("cursor") {
					if cursor == "" || len(cursor) > 16384 {
						return fmt.Errorf("invalid cursor")
					}
					for _, flag := range []string{"service", "since", "until", "limit"} {
						if cmd.Flags().Changed(flag) {
							return fmt.Errorf("--cursor cannot be combined with page filters")
						}
					}
				} else {
					now := time.Now().UTC()
					start, end, err := resolveBoundedWindow(since, until, cmd.Flags().Changed("until"), now)
					if err != nil {
						return err
					}
					if !validBoundedWindow(start, end, now) || limit < 1 || limit > storage.MaxRecentLimit {
						return fmt.Errorf("invalid page window or limit (limit must be 1 to %d)", storage.MaxRecentLimit)
					}
					if cmd.Flags().Changed("service") && (service == "" || len(service) > 1024 || !utf8.ValidString(service)) {
						return fmt.Errorf("invalid --service")
					}
					req.Service, req.Since, req.Until, req.Limit = service, start, end, limit
				}
				var result storage.PageResult
				err := withDatabase(func(_ *cobra.Command, _ []string, db *sql.DB) error {
					var err error
					result, err = storage.QueryPage(db, req)
					return err
				})(cmd, args)
				if err != nil {
					return err
				}
				return writeJSON(cmd, result, false)
			},
		}
		cmd.Flags().StringVar(&service, "service", "", "filter by service.name")
		cmd.Flags().StringVar(&since, "since", "", "inclusive start (RFC3339 or positive duration; required for first page)")
		cmd.Flags().StringVar(&until, "until", "", "inclusive end (RFC3339, default now)")
		cmd.Flags().IntVar(&limit, "limit", 0, "records per page (required for first page, 1 to 1000)")
		cmd.Flags().StringVar(&cursor, "cursor", "", "continuation cursor from the previous page")
		parent.AddCommand(cmd)
	}
	return parent
}
