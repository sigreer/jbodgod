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
		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		drives := drive.GetAll(cfg)
		drive.PrintStatus(drives)
	},
}

var jsonCmd = &cobra.Command{
	Use:   "json",
	Short: "Output drive info as JSON",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		drives := drive.GetAll(cfg)
		drive.PrintJSON(drives)
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
	Run: func(cmd *cobra.Command, args []string) {
		interval, _ := cmd.Flags().GetInt("interval")
		cfg, err := config.Load(cfgFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		drive.Monitor(cfg, interval)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is /etc/jbodgod/config.yaml)")

	monitorCmd.Flags().IntP("interval", "i", 5, "refresh interval in seconds")

	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(jsonCmd)
	rootCmd.AddCommand(spindownCmd)
	rootCmd.AddCommand(spinupCmd)
	rootCmd.AddCommand(monitorCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
