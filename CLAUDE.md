# JBODgod - Claude Code Context

## Project Overview

JBODgod is a CLI tool for managing JBOD (Just-a-Bunch-Of-Disks) enclosures and storage drives on Linux. It provides power management, monitoring, enclosure LED control, and integration APIs for SAS/SATA drives with ZFS and LVM support.

**Target users:** System administrators managing enterprise storage arrays.

## Architecture

```
app/
├── cmd/jbodgod/          # CLI entry point (Cobra)
│   ├── main.go           # Root command and subcommand registration
│   ├── status.go         # status command - drive state/temp display
│   ├── locate.go         # locate command - enclosure LED control
│   ├── identify.go       # identify command - universal device lookup
│   ├── detail.go         # detail command - controller/device queries
│   ├── inventory.go      # inventory command - database management
│   └── healthcheck.go    # healthcheck command - system health
├── internal/
│   ├── config/           # YAML configuration loading
│   ├── drive/            # Drive operations (status, spindown, spinup, monitor)
│   ├── hba/              # HBA controller discovery (storcli, sas3ircu)
│   ├── ses/              # SES enclosure LED control (sg_ses)
│   ├── zfs/              # ZFS pool health monitoring
│   ├── db/               # SQLite inventory database
│   ├── cache/            # TTL-based caching system
│   └── identify/         # Universal device identification
├── go.mod
└── go.sum
```

## Key External Tools

The app shells out to these Linux utilities (most require root):

| Tool | Package | Purpose |
|------|---------|---------|
| `smartctl` | smartmontools | SMART data, drive state, temperature |
| `sdparm` | sdparm | SCSI power management (spindown/spinup) |
| `lsscsi` | lsscsi | SCSI device enumeration, SG device mapping |
| `sg_ses` | sg3-utils | SES enclosure LED control |
| `zpool` | zfsutils-linux | ZFS pool status |
| `lsblk` | util-linux | Block device info |
| `storcli` | (vendor) | LSI/Broadcom HBA queries |
| `sas3ircu` | (vendor) | SAS adapter queries |

## Commands

| Command | Description |
|---------|-------------|
| `status` | Display drive states and temperatures |
| `monitor -i N` | Live TUI monitoring with N-second refresh |
| `spindown` / `spinup` | Power management for all drives |
| `locate <id>` | Flash enclosure bay LED for physical drive location |
| `identify <query>` | Universal device lookup (serial, WWN, GUID, etc.) |
| `detail <target>` | Query controller or device details |
| `inventory list\|sync\|show` | Drive inventory database management |
| `healthcheck` | System health validation |

## Coding Conventions

- **Concurrency:** Use goroutines with WaitGroups for parallel drive queries
- **Caching:** TTL-based singleton cache (TTLStatic=24h, TTLSlow=1h, TTLFast=5s)
- **JSON output:** Must be valid and parseable; use `--json` flag consistently
- **Null handling:** JSON null for unavailable data (standby drives don't report temp)
- **Config:** YAML with baked-in defaults; searched in /etc, ~/.config, ./config.yaml
- **Errors:** Return meaningful error messages; graceful fallbacks where possible
- **Database:** SQLite with WAL mode; optional (tool works without it)

## Testing

```bash
cd app && go build -o jbodgod ./cmd/jbodgod
sudo ./jbodgod status       # Basic status
sudo ./jbodgod monitor      # Live monitoring
sudo ./jbodgod identify /dev/sda  # Device lookup
```

Requires root for smartctl/sdparm/sg_ses access.

## Database

Location: `/var/lib/jbodgod/inventory.db` (SQLite)

Tables:
- `drives` - Drive inventory with location, serial, state
- `drive_events` - State transition history
- `zfs_health` - Pool health snapshots
- `alerts` - Alert history with acknowledgment

## Key Types

```go
// internal/drive/DriveInfo
type DriveInfo struct {
    Device    string  // /dev/sdX
    State     string  // active, standby, missing, failed
    Temp      *int    // Celsius, nil if unknown
    Serial    string
    LUID      string
    SCSIAddr  string
    Model     string
    Zpool     string
    Vdev      string
}

// internal/hba/ControllerInfo
type ControllerInfo struct {
    ID          string
    Type        string  // storcli, sas3ircu
    Model       string
    Temperature *int
    Enclosures  []EnclosureInfo
    Devices     []PhysicalDevice
}

// internal/ses/LocateInfo
type LocateInfo struct {
    Query       string
    MatchedAs   string  // identifier type used
    DevicePath  string
    Serial      string
    EnclosureID int
    Slot        int
    SGDevice    string  // /dev/sgX for LED control
}
```

## Configuration Example

```yaml
enclosures:
  - name: jbod1
    drives:
      - name: bay1
        device: /dev/sdh
      # ... more drives

thresholds:
  warning_temp: 55
  critical_temp: 60

alerts:
  email: admin@example.com
  webhook: http://localhost:8080/alerts
```

## Important Implementation Notes

1. **Drive state detection:** `smartctl -n standby` returns "NOT READY" for standby drives without waking them
2. **SES discovery:** Maps HBA enclosure IDs to /dev/sg* devices via lsscsi -g
3. **Serial matching:** HBA may report truncated serials; match both short and VPD serials
4. **Locate fallback:** For failed/missing drives, check inventory DB for last-known location
5. **ZFS integration:** Uses zpool list -v for vdev membership detection

## Build

```bash
cd app
go build -o jbodgod ./cmd/jbodgod
sudo mv jbodgod /usr/local/bin/
```

Go version: 1.25+
