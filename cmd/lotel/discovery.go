package main

import (
	"database/sql"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

func discoveryCommand(name string) *cobra.Command {
	var service, since, until string
	var limit int
	cmd := &cobra.Command{
		Use:   name,
		Short: "Discover observed " + name + " (JSON output)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := parseQueryOpts(service, since, until, limit)
			if err != nil {
				return err
			}
			discoveryOpts := storage.DiscoveryOptions{Service: opts.Service, Since: opts.Since, Until: opts.Until, Limit: opts.Limit}
			if err := discoveryOpts.Validate(); err != nil {
				return err
			}
			return withDatabase(func(cmd *cobra.Command, args []string, db *sql.DB) error {
				if name == "services" {
					results, err := storage.QueryServices(db, discoveryOpts)
					if err != nil {
						return err
					}
					printJSON(results)
					return nil
				}
				results, err := storage.QueryOperations(db, discoveryOpts)
				if err != nil {
					return err
				}
				printJSON(results)
				return nil
			})(cmd, args)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "filter by service.name")
	cmd.Flags().StringVar(&since, "since", "", "start time (RFC3339 or relative like '1h', '24h')")
	cmd.Flags().StringVar(&until, "until", "", "end time (RFC3339)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results (0 = unlimited)")
	return cmd
}
