# JBODgod

A CLI tool for managing JBOD (Just-a-Bunch-Of-Disks) enclosures and storage drives on Linux. Provides drive status monitoring, power management, enclosure LED control, and health checking for SAS/SATA drives with ZFS and LVM support.

## Features

- **Drive Monitoring** - Real-time status, temperature, and state monitoring
- **Power Management** - Spin down/up drives for power savings
- **Enclosure LED Control** - Flash bay LEDs to physically locate drives
- **Universal Identification** - Find drives by serial, WWN, GUID, device path, or 25+ other identifiers
- **HBA Integration** - Works with LSI/Broadcom (storcli) and SAS (sas3ircu) controllers
- **ZFS Pool Awareness** - Shows pool membership and health status
- **Inventory Database** - Track drive history, state changes, and alerts
- **JSON API Output** - Machine-readable output for integrations

## Installation

### From Source

```bash
cd app
go build -o jbodgod ./cmd/jbodgod
sudo mv jbodgod /usr/local/bin/
```

### Requirements

**Required packages:**
```bash
# Debian/Ubuntu
sudo apt install smartmontools sdparm lsscsi sg3-utils

# Arch Linux
sudo pacman -S smartmontools sdparm lsscsi sg3_utils

# RHEL/CentOS
sudo dnf install smartmontools sdparm lsscsi sg3_utils
```

**Optional (for HBA features):**
- `storcli` - For LSI/Broadcom RAID controllers
- `sas3ircu` - For SAS HBA controllers
- ZFS utilities (`zpool`, `zfs`) - For ZFS pool integration

## Usage

Most commands require root privileges for hardware access.

### Show Drive Status

```bash
sudo jbodgod status              # Table output
sudo jbodgod status --json       # JSON output
```

### Live Monitoring

```bash
sudo jbodgod monitor             # Default 2s refresh
sudo jbodgod monitor -i 5        # 5-second refresh
sudo jbodgod monitor -t 60       # Temperature refresh every 60s
sudo jbodgod monitor -c 0        # Include controller 0 temperature
```

### Power Management

JBODgod provides ZFS-aware power management. When spinning down drives that are part of a ZFS pool, you'll be prompted to gracefully export the pool first. This prevents data corruption and enables automatic pool re-import on spinup.

```bash
# Basic spindown (requires controller or device specification)
sudo jbodgod spindown -c c0              # Spin down all drives on controller c0
sudo jbodgod spindown /dev/sda           # Spin down specific drive
sudo jbodgod spindown /dev/sda /dev/sdb  # Spin down multiple drives

# ZFS handling options
sudo jbodgod spindown --force-all -c c0  # Export all pools without prompts
sudo jbodgod spindown --force /dev/sda   # Skip ZFS checks entirely (dangerous!)

# Spinup with automatic pool re-import
sudo jbodgod spinup -c c0                # Spin up drives, auto-import pools
sudo jbodgod spinup --no-import -c c0    # Spin up without pool re-import
```

**ZFS Workflow:**
1. `spindown` detects if target drives are part of ZFS pools
2. Prompts for each pool: "Export pool 'tank'? [y/n]"
3. If yes: runs `sync` → `zpool sync` → `zpool export`
4. Records export in database for spinup
5. Spins down drives

6. `spinup` brings drives online
7. Checks database for previously exported pools
8. Automatically imports matching pools

### Locate a Drive (Flash Enclosure LED)

```bash
# By various identifiers
sudo jbodgod locate /dev/sda              # By device path
sudo jbodgod locate WCK5NWKQ              # By serial number
sudo jbodgod locate 2:5                   # By enclosure:slot
sudo jbodgod locate 0x5000c500d006891c    # By WWN

# Control options
sudo jbodgod locate --timeout 60s /dev/sda   # Flash for 60 seconds
sudo jbodgod locate --on /dev/sda            # Turn LED on (stays on)
sudo jbodgod locate --off /dev/sda           # Turn LED off
sudo jbodgod locate --info-only /dev/sda     # Show location info only
sudo jbodgod locate --json /dev/sda          # JSON output
```

### Identify a Device

```bash
# Find a device by any identifier
sudo jbodgod identify /dev/sda
sudo jbodgod identify ZA1DKJT7                     # Serial number
sudo jbodgod identify 5000c500d006891c             # WWN or LUID
sudo jbodgod identify 1234567890abcdef             # ZFS vdev GUID
sudo jbodgod identify --output json /dev/sda       # JSON output
```

### Query Controller/Device Details

```bash
sudo jbodgod detail c0                    # Controller 0 info
sudo jbodgod detail c0 temperature        # Controller temperature
sudo jbodgod detail c0 devices            # Attached devices
sudo jbodgod detail 2:5                   # Device at enclosure 2, slot 5
sudo jbodgod detail serial:WCK5NWKQ       # Device by serial
```

### Inventory Management

```bash
sudo jbodgod inventory list               # List all known drives
sudo jbodgod inventory sync               # Sync current state to database
sudo jbodgod inventory show WCK5NWKQ      # Show drive details
sudo jbodgod inventory events             # Show recent events
sudo jbodgod inventory alerts             # Show unacknowledged alerts
```

### Health Check

```bash
sudo jbodgod healthcheck                  # Text output
sudo jbodgod healthcheck --json           # JSON output
```

## Configuration

Copy `config.example.yaml` to one of these locations:

1. `/etc/jbodgod/config.yaml` (system-wide)
2. `~/.config/jbodgod/config.yaml` (user)
3. `./config.yaml` (current directory)

Or specify with `--config /path/to/config.yaml`.

### Example Configuration

```yaml
enclosures:
  - name: jbod1
    drives:
      - name: bay1
        device: /dev/sdh
      - name: bay2
        device: /dev/sdi
      # ... more drives

thresholds:
  warning_temp: 55
  critical_temp: 60
  action_on_critical: alert  # alert, spindown, or notify

alerts:
  email: admin@example.com
  webhook: http://localhost:8080/alerts
```

## Database

JBODgod maintains a SQLite database at `/var/lib/jbodgod/inventory.db` for:

- **Drive inventory** - All drives ever seen, with serial, model, location
- **State history** - When drives came online, went offline, failed
- **ZFS health snapshots** - Pool status over time
- **Exported pools** - Tracks ZFS pools exported during spindown for automatic re-import
- **Alerts** - Temperature warnings, failures, with acknowledgment tracking

The database is optional - all commands work without it, but `inventory`, `healthcheck`, and automatic pool re-import features require it.

## Drive States

| State | Description |
|-------|-------------|
| `active` | Drive is spinning and responsive |
| `standby` | Drive is spun down (power saving) |
| `missing` | Device path doesn't exist |
| `failed` | Device exists but not responding |

## Output Formats

All commands support `--json` for machine-readable output:

```bash
sudo jbodgod status --json | jq '.drives[] | select(.state == "active")'
sudo jbodgod locate --json /dev/sda | jq '.slot'
```

## Project Structure

```
app/
├── cmd/jbodgod/       # CLI commands
├── internal/
│   ├── config/        # YAML configuration
│   ├── drive/         # Drive operations
│   ├── hba/           # HBA controller integration
│   ├── ses/           # Enclosure LED control
│   ├── zfs/           # ZFS pool health
│   ├── db/            # SQLite inventory
│   ├── cache/         # TTL-based caching
│   └── identify/      # Device identification
├── go.mod
└── go.sum
```

## Troubleshooting

### Permission Denied

Most commands require root. Use `sudo` or run as root.

### SES Device Not Found

Ensure `sg3-utils` is installed and check that your enclosure supports SES:

```bash
lsscsi -g | grep enclosure
```

### HBA Not Detected

Check that `storcli` or `sas3ircu` is installed and accessible:

```bash
which storcli || which sas3ircu
sudo storcli /c0 show || sudo sas3ircu 0 display
```

### Drive Not Responding

Check SMART status:

```bash
sudo smartctl -a /dev/sdX
```

## License

MIT
