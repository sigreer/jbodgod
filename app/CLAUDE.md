# JBODgod - Claude Code Context

## Project Overview

JBODgod is a CLI tool for managing JBOD enclosures and storage drives on Linux. It provides power management, monitoring, and integration APIs for SAS/SATA drives with ZFS and LVM support.

## Architecture

- **cmd/jbodgod/main.go**: CLI entry point using Cobra
- **internal/config/**: YAML configuration loading
- **internal/drive/**: Core drive operations (status, spindown, spinup, monitor)
- **internal/storage/**: ZFS/LVM pool management (planned)
- **internal/monitor/**: Background monitoring daemon (planned)
- **internal/alerts/**: Temperature thresholds and notifications (planned)

## Key External Tools

The app shells out to these Linux utilities:
- `smartctl` (smartmontools) - SMART data, drive state detection
- `sdparm` - SCSI power management (spindown/spinup)
- `lsscsi` - SCSI device enumeration
- `zpool` - ZFS pool status
- `lsblk` - Block device info

## Conventions

- Use goroutines for parallel drive queries (12 drives = 12 concurrent queries)
- JSON output must be valid and parseable by sidebar widgets
- Null values in JSON for unavailable data (standby drives don't report temp/serial)
- Config uses YAML with sensible defaults baked into the binary

## Current State

Working features:
- `status` - Table output of drive states
- `json` - Full JSON output with zpool/vdev/serial/scsi_addr
- `monitor` - Live TUI with temperature status
- `spindown` / `spinup` - Async power management with progress

## Testing

```bash
go build -o jbodgod ./cmd/jbodgod
sudo ./jbodgod json
```

Requires root for smartctl/sdparm access.

## Future Work

- SQLite for historical data
- Webhook/email alerts
- LVM VG support
- Enclosure LED control (sg_ses)
- systemd service for background monitoring
