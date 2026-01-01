package main

import (
	"fmt"
	"os"

	"github.com/sigreer/jbodgod/internal/identify"
	"github.com/spf13/cobra"
)

var identifyCmd = &cobra.Command{
	Use:   "identify <query>",
	Short: "Look up device by any unique identifier",
	Long: `Query any unique device identifier to retrieve all related identifiers.

Supports: device paths, serial numbers, WWN, UUID, ZFS GUIDs, LVM UUIDs,
partition UUIDs, filesystem labels, and more.

Examples:
  jbodgod identify /dev/sda
  jbodgod identify WCK5NWKQ                    # Serial number
  jbodgod identify 0x5000c500d006891c          # WWN
  jbodgod identify 14707061191158689053        # ZFS pool GUID
  jbodgod identify tank                        # ZFS pool name
  jbodgod identify 2f4ca112-c476-...           # GPT Partition UUID`,
	Args: cobra.ExactArgs(1),
	Run:  runIdentify,
}

func init() {
	identifyCmd.Flags().StringP("output", "o", "json", "Output format: json, table")
	identifyCmd.Flags().BoolP("quiet", "q", false, "Only output device path")
}

func runIdentify(cmd *cobra.Command, args []string) {
	query := args[0]
	outputFmt, _ := cmd.Flags().GetString("output")
	quiet, _ := cmd.Flags().GetBool("quiet")

	// Build the device index
	idx, err := identify.BuildIndex()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building device index: %v\n", err)
		os.Exit(1)
	}

	// Look up the query
	entity, matchedAs, err := idx.Lookup(query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Not found: %s\n", query)
		os.Exit(1)
	}

	// Create result
	result := &identify.LookupResult{
		Query:     query,
		MatchedAs: matchedAs,
		Device:    entity,
	}

	// Output based on format
	if quiet {
		identify.PrintQuiet(os.Stdout, result)
		return
	}

	switch outputFmt {
	case "table":
		identify.PrintTable(os.Stdout, result)
	default:
		if err := identify.PrintJSON(os.Stdout, result); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding output: %v\n", err)
			os.Exit(1)
		}
	}
}
