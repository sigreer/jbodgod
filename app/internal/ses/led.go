package ses

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CheckSgSesInstalled verifies sg_ses is available
func CheckSgSesInstalled() error {
	if _, err := exec.LookPath("sg_ses"); err != nil {
		return ErrSgSesNotInstalled
	}
	return nil
}

// SetSlotIdentLED turns the identify LED on or off for a specific slot
// sgDevice: /dev/sg<N>
// slot: slot number
// on: true to turn on, false to turn off
func SetSlotIdentLED(sgDevice string, slot int, on bool) error {
	if err := CheckSgSesInstalled(); err != nil {
		return err
	}

	action := "--clear=ident"
	if on {
		action = "--set=ident"
	}

	cmd := exec.Command("sudo", "sg_ses",
		fmt.Sprintf("--dev-slot-num=%d", slot),
		action,
		sgDevice,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(out)
		// Check for permission errors
		if strings.Contains(strings.ToLower(outStr), "permission denied") ||
			strings.Contains(strings.ToLower(outStr), "operation not permitted") {
			return ErrPermissionDenied
		}
		return fmt.Errorf("sg_ses failed: %s: %w", strings.TrimSpace(outStr), err)
	}

	return nil
}

// SetSlotFaultLED turns the fault LED on or off
func SetSlotFaultLED(sgDevice string, slot int, on bool) error {
	if err := CheckSgSesInstalled(); err != nil {
		return err
	}

	action := "--clear=fault"
	if on {
		action = "--set=fault"
	}

	cmd := exec.Command("sudo", "sg_ses",
		fmt.Sprintf("--dev-slot-num=%d", slot),
		action,
		sgDevice,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sg_ses failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return nil
}

// GetSlotLEDState retrieves the current LED state for a slot
func GetSlotLEDState(sgDevice string, slot int) (*SlotLEDState, error) {
	if err := CheckSgSesInstalled(); err != nil {
		return nil, err
	}

	cmd := exec.Command("sudo", "sg_ses",
		"--page=es", // Element status page
		"--join",    // Join with element descriptor page
		sgDevice,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sg_ses failed: %w", err)
	}

	state := &SlotLEDState{Slot: slot}
	outStr := strings.ToLower(string(out))

	// Parse LED states from output
	// Looking for patterns like "ident=1", "fault=1", etc.
	state.Ident = strings.Contains(outStr, "ident=1") || strings.Contains(outStr, "identify=1")
	state.Fault = strings.Contains(outStr, "fault=1")
	state.Active = strings.Contains(outStr, "active=1")

	return state, nil
}

// LocateWithTimeout turns on the locate LED for a specified duration
// then automatically turns it off
func LocateWithTimeout(ctx context.Context, sgDevice string, slot int, duration time.Duration) error {
	// Turn on the LED
	if err := SetSlotIdentLED(sgDevice, slot, true); err != nil {
		return fmt.Errorf("failed to turn on LED: %w", err)
	}

	// Wait for duration or context cancellation
	select {
	case <-time.After(duration):
		// Duration elapsed, turn off LED
	case <-ctx.Done():
		// Context cancelled, still turn off LED
	}

	// Always attempt to turn off LED
	if err := SetSlotIdentLED(sgDevice, slot, false); err != nil {
		return fmt.Errorf("failed to turn off LED: %w", err)
	}

	return nil
}

// LocateAsync starts a locate operation in a goroutine and returns immediately
// Returns a channel that receives the result when complete
func LocateAsync(sgDevice string, slot int, duration time.Duration) <-chan error {
	result := make(chan error, 1)

	go func() {
		ctx := context.Background()
		result <- LocateWithTimeout(ctx, sgDevice, slot, duration)
		close(result)
	}()

	return result
}
