package main

import (
	"encoding/json"
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type catalogFlag struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
	Inherited   bool   `json:"inherited"`
}

type catalogCommand struct {
	Path     string        `json:"path"`
	Usage    string        `json:"usage"`
	Summary  string        `json:"summary"`
	Runnable bool          `json:"runnable"`
	Flags    []catalogFlag `json:"flags"`
}

type commandCatalog struct {
	SchemaVersion int              `json:"schema_version"`
	Version       string           `json:"version"`
	Commands      []catalogCommand `json:"commands"`
}

func buildCommandCatalog(root *cobra.Command, binaryVersion string) commandCatalog {
	catalog := commandCatalog{SchemaVersion: 1, Version: binaryVersion, Commands: []catalogCommand{}}
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Hidden {
			return
		}
		entry := catalogCommand{Path: cmd.CommandPath(), Usage: cmd.Use, Summary: cmd.Short, Runnable: cmd.Runnable(), Flags: []catalogFlag{}}
		addFlags := func(flags *pflag.FlagSet, inherited bool) {
			flags.VisitAll(func(flag *pflag.Flag) {
				if !flag.Hidden {
					entry.Flags = append(entry.Flags, catalogFlag{Name: flag.Name, Shorthand: flag.Shorthand, Type: flag.Value.Type(), Default: flag.DefValue, Description: flag.Usage, Inherited: inherited})
				}
			})
		}
		addFlags(cmd.LocalFlags(), false)
		addFlags(cmd.InheritedFlags(), true)
		sort.Slice(entry.Flags, func(i, j int) bool { return entry.Flags[i].Name < entry.Flags[j].Name })
		catalog.Commands = append(catalog.Commands, entry)
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
	sort.Slice(catalog.Commands, func(i, j int) bool { return catalog.Commands[i].Path < catalog.Commands[j].Path })
	return catalog
}

func commandsCommand(catalog *commandCatalog) *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "List registered commands and flags (JSON)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(catalog)
		},
	}
}
