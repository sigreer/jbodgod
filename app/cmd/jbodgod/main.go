package main

import (
	"fmt"
	"os"

	"github.com/sigreer/jbodgod/internal/config"
	"github.com/sigreer/jbodgod/internal/drive"
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

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show drive states and temperatures",
	Run: func(cmd *cobra.Command, args []string) {
		jsonOut, _ := cmd.Flags().GetBool("json")
		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		drives := drive.GetAll(cfg)
		if jsonOut {
			controllers, enclosures, _ := drive.FetchHBAData(false)
			drive.PrintJSON(drives, controllers, enclosures)
		} else {
			drive.PrintStatus(drives)
		}
	},
}

var spindownCmd = &cobra.Command{
	Use:   "spindown",
	Short: "Spin down all drives",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		drive.Spindown(cfg)
	},
}

var spinupCmd = &cobra.Command{
	Use:   "spinup",
	Short: "Spin up all drives",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		drive.Spinup(cfg)
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

	monitorCmd.Flags().IntP("interval", "i", 2, "state refresh interval in seconds")
	monitorCmd.Flags().IntP("temp-interval", "t", 30, "temperature refresh interval in seconds")
	monitorCmd.Flags().StringP("controller", "c", "", "controller to monitor (e.g., c0)")

	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(spindownCmd)
	rootCmd.AddCommand(spinupCmd)
	rootCmd.AddCommand(monitorCmd)
	rootCmd.AddCommand(identifyCmd)
	rootCmd.AddCommand(detailCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
