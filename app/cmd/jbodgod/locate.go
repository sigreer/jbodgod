package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sigreer/jbodgod/internal/db"
	"github.com/sigreer/jbodgod/internal/ses"
	"github.com/spf13/cobra"
)

// LocateResponse is the JSON response structure for application integration
type LocateResponse struct {
	Success     bool    `json:"success"`
	Action      string  `json:"action"`                 // "on", "off", "timed", "info"
	LEDState    string  `json:"led_state"`              // "on", "off"
	Device      string  `json:"device"`
	Serial      string  `json:"serial"`
	Model       string  `json:"model,omitempty"`
	Enclosure   int     `json:"enclosure"`
	Slot        int     `json:"slot"`
	SGDevice    string  `json:"sg_device"`
	MatchedAs   string  `json:"matched_as,omitempty"`
	Duration    float64 `json:"duration_seconds,omitempty"` // How long LED was on
	StopReason  string  `json:"stop_reason,omitempty"`      // "timeout", "interrupted", "manual"
	Timestamp   string  `json:"timestamp"`
	Error       string  `json:"error,omitempty"`
}

var locateCmd = &cobra.Command{
	Use:   "locate <identifier>",
	Short: "Flash the enclosure bay LED for a drive",
	Long: `Flash the identify LED on a drive's enclosure bay to help locate it physically.

The identifier can be any unique device identifier:
  - Device path: /dev/sda, /dev/disk/by-id/...
  - Serial number: WCK5NWKQ
  - Enclosure:Slot: 2:5 (directly specify bay location)
  - WWN: 0x5000c500d006891c
  - LUID: 5000c500d006891c
  - ZFS pool/vdev GUID
  - Partition UUID
  - And many more...

For failed/missing drives, the command will:
  1. Try live device lookup first
  2. Check inventory database for last-known location
  3. Support enclosure:slot format for direct bay access

Modes:
  (default)    Flash LED for --timeout duration, then turn off
  --on         Turn LED on and exit (for external app control)
  --off        Turn LED off
  --info-only  Show device location without changing LED

The --json flag provides machine-readable output for application integration.

Examples:
  jbodgod locate /dev/sda                    # Flash for 30s
  jbodgod locate --timeout 60s ZA1DKJT7      # Flash for 60s
  jbodgod locate 2:5                         # Locate by enclosure 2, slot 5
  jbodgod locate --on --json /dev/sda        # Turn on, output JSON
  jbodgod locate --off --json /dev/sda       # Turn off, output JSON
  jbodgod locate --info-only --json /dev/sda # Get location info as JSON`,
	Args: cobra.ExactArgs(1),
	Run:  runLocate,
}

func init() {
	locateCmd.Flags().DurationP("timeout", "t", 30*time.Second, "LED flash duration (e.g., 30s, 1m)")
	locateCmd.Flags().BoolP("verbose", "v", false, "Show detailed progress output")
	locateCmd.Flags().Bool("json", false, "Output result as JSON (for application integration)")
	locateCmd.Flags().Bool("info-only", false, "Only show device location info, don't change LED")
	locateCmd.Flags().Bool("on", false, "Turn LED on and exit immediately (for external control)")
	locateCmd.Flags().Bool("off", false, "Turn LED off")
}

func runLocate(cmd *cobra.Command, args []string) {
	query := args[0]
	timeout, _ := cmd.Flags().GetDuration("timeout")
	verbose, _ := cmd.Flags().GetBool("verbose")
	jsonOut, _ := cmd.Flags().GetBool("json")
	infoOnly, _ := cmd.Flags().GetBool("info-only")
	turnOn, _ := cmd.Flags().GetBool("on")
	turnOff, _ := cmd.Flags().GetBool("off")

	// Check for sg_ses before doing anything
	if err := ses.CheckSgSesInstalled(); err != nil {
		if jsonOut {
			outputError("sg_ses not found - install sg3_utils package", nil)
		} else {
			fmt.Fprintf(os.Stderr, "Error: sg_ses not found.\n")
			fmt.Fprintf(os.Stderr, "Install: sudo pacman -S sg3_utils lsscsi  (Arch)\n")
			fmt.Fprintf(os.Stderr, "     or: sudo apt install sg3-utils lsscsi  (Debian/Ubuntu)\n")
		}
		os.Exit(1)
	}

	// Try to open database for fallback lookups (optional - don't fail if unavailable)
	var database *db.DB
	database, _ = db.New(db.DefaultPath)
	if database != nil {
		defer database.Close()
	}

	// Get device info using fallback logic (supports enclosure:slot, DB serial lookup)
	info, err := ses.GetLocateInfoWithFallback(query, database)
	if err != nil {
		if jsonOut {
			outputError(err.Error(), info)
		} else {
			if info != nil && info.DevicePath != "" {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				fmt.Fprintf(os.Stderr, "Device: %s (serial: %s)\n", info.DevicePath, info.Serial)
			} else {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		}
		os.Exit(1)
	}

	// Validate we have all needed info
	if info.SGDevice == "" {
		errMsg := "Could not find SES device for enclosure (try: sudo modprobe sg)"
		if jsonOut {
			outputError(errMsg, info)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
			fmt.Fprintf(os.Stderr, "Device: %s (serial: %s)\n", info.DevicePath, info.Serial)
			fmt.Fprintf(os.Stderr, "Enclosure: %d, Slot: %d\n", info.EnclosureID, info.Slot)
		}
		os.Exit(1)
	}

	// Info-only mode: just display location and exit
	if infoOnly {
		resp := buildResponse(info, "info", "unknown", "", 0)
		if jsonOut {
			outputJSON(resp)
		} else {
			fmt.Printf("Device:     %s\n", info.DevicePath)
			fmt.Printf("Matched As: %s\n", info.MatchedAs)
			fmt.Printf("Serial:     %s\n", info.Serial)
			if info.Model != "" {
				fmt.Printf("Model:      %s\n", info.Model)
			}
			fmt.Printf("Enclosure:  %d\n", info.EnclosureID)
			fmt.Printf("Slot:       %d\n", info.Slot)
			fmt.Printf("SG Device:  %s\n", info.SGDevice)
		}
		return
	}

	// Turn off mode
	if turnOff {
		if verbose {
			fmt.Printf("Turning off LED for enclosure %d, slot %d...\n", info.EnclosureID, info.Slot)
		}
		if err := ses.SetSlotIdentLED(info.SGDevice, info.Slot, false); err != nil {
			if jsonOut {
				resp := buildResponse(info, "off", "off", "", 0)
				resp.Success = false
				resp.Error = err.Error()
				outputJSON(resp)
			} else {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
			os.Exit(1)
		}
		resp := buildResponse(info, "off", "off", "manual", 0)
		if jsonOut {
			outputJSON(resp)
		} else {
			fmt.Printf("LED OFF for %s (enc:%d slot:%d)\n", info.DevicePath, info.EnclosureID, info.Slot)
		}
		return
	}

	// Turn on mode (no timeout, just turn on and exit)
	if turnOn {
		if verbose {
			fmt.Printf("Turning on LED for enclosure %d, slot %d...\n", info.EnclosureID, info.Slot)
		}
		if err := ses.SetSlotIdentLED(info.SGDevice, info.Slot, true); err != nil {
			if jsonOut {
				resp := buildResponse(info, "on", "off", "", 0)
				resp.Success = false
				resp.Error = err.Error()
				outputJSON(resp)
			} else {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
			os.Exit(1)
		}
		resp := buildResponse(info, "on", "on", "", 0)
		if jsonOut {
			outputJSON(resp)
		} else {
			fmt.Printf("LED ON for %s (enc:%d slot:%d)\n", info.DevicePath, info.EnclosureID, info.Slot)
		}
		return
	}

	// Timed locate mode (default)
	if verbose {
		fmt.Printf("Locating: %s\n", query)
		fmt.Printf("  Device:    %s\n", info.DevicePath)
		fmt.Printf("  Serial:    %s\n", info.Serial)
		fmt.Printf("  Enclosure: %d, Slot: %d\n", info.EnclosureID, info.Slot)
		fmt.Printf("  SG Device: %s\n", info.SGDevice)
		fmt.Printf("  Duration:  %v\n", timeout)
		fmt.Println()
	}

	// Turn on LED
	if err := ses.SetSlotIdentLED(info.SGDevice, info.Slot, true); err != nil {
		if jsonOut {
			resp := buildResponse(info, "timed", "off", "", 0)
			resp.Success = false
			resp.Error = "failed to turn on LED: " + err.Error()
			outputJSON(resp)
		} else {
			fmt.Fprintf(os.Stderr, "Error turning on LED: %v\n", err)
		}
		os.Exit(1)
	}

	startTime := time.Now()

	if jsonOut {
		// Output initial "on" state
		resp := buildResponse(info, "timed", "on", "", 0)
		outputJSON(resp)
	} else {
		fmt.Printf("LED ON for %s (enc:%d slot:%d) - will turn off in %v\n",
			info.DevicePath, info.EnclosureID, info.Slot, timeout)
	}

	// Set up signal handling for Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Wait for timeout or interrupt
	stopReason := "timeout"
	select {
	case <-ctx.Done():
		// Timeout elapsed
		stopReason = "timeout"
	case <-sigChan:
		stopReason = "interrupted"
		if !jsonOut {
			fmt.Println("\nInterrupted, turning off LED...")
		}
	}

	// Turn off LED
	if err := ses.SetSlotIdentLED(info.SGDevice, info.Slot, false); err != nil {
		if jsonOut {
			resp := buildResponse(info, "timed", "on", stopReason, time.Since(startTime).Seconds())
			resp.Success = false
			resp.Error = "failed to turn off LED: " + err.Error()
			outputJSON(resp)
		} else {
			fmt.Fprintf(os.Stderr, "Error turning off LED: %v\n", err)
		}
		os.Exit(1)
	}

	duration := time.Since(startTime)

	if jsonOut {
		resp := buildResponse(info, "timed", "off", stopReason, duration.Seconds())
		outputJSON(resp)
	} else {
		fmt.Printf("LED OFF (was on for %v)\n", duration.Round(time.Second))
	}
}

func buildResponse(info *ses.LocateInfo, action, ledState, stopReason string, duration float64) *LocateResponse {
	resp := &LocateResponse{
		Success:   true,
		Action:    action,
		LEDState:  ledState,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if info != nil {
		resp.Device = info.DevicePath
		resp.Serial = info.Serial
		resp.Model = info.Model
		resp.Enclosure = info.EnclosureID
		resp.Slot = info.Slot
		resp.SGDevice = info.SGDevice
		resp.MatchedAs = info.MatchedAs
	}
	if stopReason != "" {
		resp.StopReason = stopReason
	}
	if duration > 0 {
		resp.Duration = duration
	}
	return resp
}

func outputJSON(resp *LocateResponse) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(resp)
}

func outputError(errMsg string, info *ses.LocateInfo) {
	resp := &LocateResponse{
		Success:   false,
		Action:    "error",
		LEDState:  "unknown",
		Error:     errMsg,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if info != nil {
		resp.Device = info.DevicePath
		resp.Serial = info.Serial
		resp.Enclosure = info.EnclosureID
		resp.Slot = info.Slot
		resp.SGDevice = info.SGDevice
	}
	outputJSON(resp)
}
