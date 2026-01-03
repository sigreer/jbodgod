package main

import (
	"fmt"
	"os"

	"github.com/sigreer/jbodgod/internal/config"
	"github.com/sigreer/jbodgod/internal/drive"
	"github.com/sigreer/jbodgod/internal/hba"
	"github.com/sigreer/jbodgod/internal/version"
	"github.com/spf13/cobra"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "jbodgod",
	Short: "JBOD and storage drive management tool",
	Long: `JBODgod is a CLI tool for managing JBOD enclosures, SAS/SATA drives,
and storage pools (ZFS, LVM). It provides monitoring, power management,
and alerting capabilities.`,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show jbodgod version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("jbodgod version %s\n", version.Version)
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show drive states and temperatures",
	Long: `Display drive status including state, temperature, and pool membership.

By default, shows core realtime data: device, slot, state, temperature, zpool.
Use --detail to include additional information like model, serial, and more.

The --json flag changes the output format without affecting the data shown.
Combine --json with --detail for comprehensive JSON output.

Examples:
  jbodgod status              # Core data in table format
  jbodgod status --json       # Core data in JSON format
  jbodgod status --detail     # Detailed data in table format
  jbodgod status --json --detail  # Full data in JSON format`,
	Run: func(cmd *cobra.Command, args []string) {
		jsonOut, _ := cmd.Flags().GetBool("json")
		detail, _ := cmd.Flags().GetBool("detail")
		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		drives := drive.GetAll(cfg)
		if jsonOut {
			var controllers []hba.ControllerInfo
			var enclosures []hba.EnclosureInfo
			if detail {
				controllers, enclosures, _ = drive.FetchHBAData(false)
			}
			drive.PrintJSON(drives, controllers, enclosures, detail)
		} else {
			drive.PrintStatus(drives, detail)
		}
	},
}

var spindownCmd = &cobra.Command{
	Use:   "spindown [-c controller] [devices...]",
	Short: "Spin down drives",
	Long: `Spin down drives to standby mode.

You MUST specify either a controller (-c) or specific device paths.
This is a safety measure to prevent accidental spindown of all drives.

ZFS pools are handled gracefully: if any target drives are part of a ZFS pool,
you will be prompted to export the pool before spindown. This ensures data
integrity and allows automatic re-import when drives are spun back up.

Flags:
  --force      Skip all ZFS checks and prompts (dangerous!)
  --force-all  Export all affected pools without individual prompts

Examples:
  jbodgod spindown -c c0              # Spin down all drives on controller c0
  jbodgod spindown /dev/sda           # Spin down a specific drive
  jbodgod spindown /dev/sda /dev/sdb  # Spin down multiple specific drives
  jbodgod spindown --force-all -c c0  # Export all pools and spin down without prompts`,
	Run: func(cmd *cobra.Command, args []string) {
		controller, _ := cmd.Flags().GetString("controller")
		force, _ := cmd.Flags().GetBool("force")
		forceAll, _ := cmd.Flags().GetBool("force-all")

		if controller == "" && len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Error: specify -c <controller> or device path(s)")
			fmt.Fprintln(os.Stderr, "This prevents accidental spindown of all drives.")
			fmt.Fprintln(os.Stderr, "Examples:")
			fmt.Fprintln(os.Stderr, "  jbodgod spindown -c c0")
			fmt.Fprintln(os.Stderr, "  jbodgod spindown /dev/sda /dev/sdb")
			os.Exit(1)
		}
		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		drive.SpindownWithZFS(cfg, controller, args, drive.SpindownOptions{
			Force:    force,
			ForceAll: forceAll,
		})
	},
}

var spinupCmd = &cobra.Command{
	Use:   "spinup [-c controller] [devices...]",
	Short: "Spin up drives",
	Long: `Spin up drives from standby mode.

Specify a controller (-c), specific device paths, or both.
If no arguments provided, spins up all discovered drives.

After spinning up drives, any ZFS pools that were exported during spindown
will be automatically re-imported.

Flags:
  --no-import  Skip automatic ZFS pool re-import

Examples:
  jbodgod spinup                      # Spin up all drives
  jbodgod spinup -c c0                # Spin up all drives on controller c0
  jbodgod spinup /dev/sda             # Spin up a specific drive
  jbodgod spinup --no-import -c c0    # Spin up without pool re-import`,
	Run: func(cmd *cobra.Command, args []string) {
		controller, _ := cmd.Flags().GetString("controller")
		noImport, _ := cmd.Flags().GetBool("no-import")

		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		drive.SpinupWithZFS(cfg, controller, args, drive.SpinupOptions{
			NoImport: noImport,
		})
	},
}

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Live monitoring with auto-refresh",
	Long: `Live monitoring with efficient in-place updates.

The monitor uses ANSI escape sequences to update values in-place without
clearing the screen, providing smooth real-time updates.

Drive states are checked every interval, while temperatures are fetched
less frequently to reduce drive load. Controller temperature (if specified)
is updated every 30 seconds.`,
	Run: func(cmd *cobra.Command, args []string) {
		interval, _ := cmd.Flags().GetInt("interval")
		tempInterval, _ := cmd.Flags().GetInt("temp-interval")
		controller, _ := cmd.Flags().GetString("controller")
		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		drive.Monitor(cfg, interval, tempInterval, controller)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is /etc/jbodgod/config.yaml)")

	statusCmd.Flags().Bool("json", false, "Output as JSON")
	statusCmd.Flags().BoolP("detail", "d", false, "Include detailed drive information")

	spindownCmd.Flags().StringP("controller", "c", "", "target specific controller (e.g., c0)")
	spindownCmd.Flags().Bool("force", false, "skip ZFS pool checks (dangerous)")
	spindownCmd.Flags().Bool("force-all", false, "export all affected pools without prompts")

	spinupCmd.Flags().StringP("controller", "c", "", "target specific controller (e.g., c0)")
	spinupCmd.Flags().Bool("no-import", false, "skip automatic ZFS pool re-import")

	monitorCmd.Flags().IntP("interval", "i", 2, "state refresh interval in seconds")
	monitorCmd.Flags().IntP("temp-interval", "t", 30, "temperature refresh interval in seconds")
	monitorCmd.Flags().StringP("controller", "c", "", "controller to monitor (e.g., c0)")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(spindownCmd)
	rootCmd.AddCommand(spinupCmd)
	rootCmd.AddCommand(monitorCmd)
	rootCmd.AddCommand(identifyCmd)
	rootCmd.AddCommand(detailCmd)
	rootCmd.AddCommand(locateCmd)
	rootCmd.AddCommand(inventoryCmd)
	rootCmd.AddCommand(healthcheckCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
