# JBODgod - Architecture & Current State Analysis

## Original Vision vs Current Reality

**Original scope:** JBOD enclosure management (SAS drives behind HBAs with SES)

**Current scope:** Universal drive management tool that works with:
- JBOD enclosures (SAS/SATA via HBA with SES LED control)
- Direct-attached drives (any SCSI/SATA)
- ZFS pools (health, vdev mapping)
- LVM volumes (identification only)
- MD RAID arrays (identification only)

---

## Project Structure

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
│   ├── config/           # YAML configuration loading + auto-discovery
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

---

## Core Strengths

| Area | Strength | Notes |
|------|----------|-------|
| **Universal Identification** | 40+ identifier types resolved to single device | Serial, WWN, LUID, ZFS GUID, LVM UUID, partition UUID, etc. |
| **Non-invasive State Detection** | Standby detection without spinup | `smartctl -n standby` technique |
| **Enclosure LED Control** | Full SES integration with fallbacks | Locate by any identifier, DB fallback for missing drives |
| **Inventory Tracking** | Persistent drive history | State transitions, first/last seen, location tracking |
| **Parallel Performance** | Goroutines + caching | Multi-tier TTL caching, parallel drive queries |
| **No Config Required** | Auto-discovery via lsscsi/lsblk | Works out of box, config optional |

---

## Functional Commands

| Command | Status | Quality | Description |
|---------|--------|---------|-------------|
| `status` | ✅ Complete | Production-ready | Display drive states and temperatures |
| `monitor` | ✅ Complete | Production-ready | Live TUI monitoring with configurable refresh |
| `spindown/spinup` | ✅ Complete | Works for SCSI drives | Power management via sdparm |
| `identify` | ✅ Complete | Excellent - flagship feature | Universal device lookup (40+ identifier types) |
| `locate` | ✅ Complete | Production-ready with fallbacks | Flash enclosure LED by any identifier |
| `detail` | ✅ Complete | Rich HBA/device queries | Controller and device information |
| `inventory` | ✅ Complete | Full CRUD + events + alerts | Database management |
| `healthcheck` | ✅ Complete | Comprehensive checks | System health validation |

---

## Internal Packages

### drive/ (758 lines)
Core drive information retrieval system:
- `GetAll()`: Parallel drive state/temp fetching with WaitGroups
- `getInfo()`: Single drive details via smartctl
- `Spindown()`/`Spinup()`: Power management via sdparm
- `Monitor()`: Real-time monitoring with configurable intervals
- `FetchHBAData()`: Controller/enclosure info from HBA tools

### hba/ (736 lines)
HBA controller discovery and device enumeration:
- **sas3ircu.go**: SAS3008 adapter queries
- **storcli.go**: LSI/Broadcom HBA queries
- Device lookups by serial, slot, SAS address
- Caches data with TTL-based invalidation

### ses/ (Multiple files)
SES (SCSI Enclosure Services) LED control:
- `SetSlotIdentLED()`: LED on/off via sg_ses
- `GetLocateInfo()`: Location resolution via identify + HBA
- `GetLocateInfoWithFallback()`: DB fallback for missing drives
- `MapEnclosureToSGDevice()`: Enclosure ID to /dev/sg* mapping

### identify/ (554 lines)
Universal device identification system:
- `BuildIndex()`: Parallel data collection from multiple sources
- `Lookup()`: Search all indexes for matching device
- **Data sources**: lsblk, /dev/disk/by-*, smartctl, zpool, zfs, lvdisplay, vgdisplay, pvdisplay, mdadm

### zfs/ (100+ lines)
ZFS pool health monitoring:
- `GetPoolHealth()`: Parse pool status
- `GetFaultedDevices()`: Recursive vdev search
- Parses `zpool status -vL` output

### db/ (961 lines)
SQLite inventory database:
- **drives**: Full drive specs, location, state, timestamps
- **drive_events**: State transition history
- **alerts**: Alert history with acknowledgment
- WAL mode, foreign keys, migration system

### config/ (150+ lines)
YAML configuration with auto-discovery:
- Search paths: /etc, ~/.config, ./config.yaml
- Discovery modes: auto, lsscsi, hba, static
- Thresholds: warning_temp (55°C), critical_temp (60°C)

### cache/ (166 lines)
Thread-safe TTL-based caching:
- `TTLStatic` = 24h (hardware config)
- `TTLSlow` = 1h (firmware, enclosure config)
- `TTLMedium` = 5m (ZFS pool membership)
- `TTLFast` = 5s (drive state)
- `TTLDynamic` = 30s (temperatures)

---

## External Tool Dependencies

| Tool | Package | Required | Purpose |
|------|---------|----------|---------|
| **smartctl** | drive | Yes (root) | SMART data, state, temperature |
| **lsscsi** | drive, identify, config, ses | Yes | SCSI device enumeration |
| **sdparm** | drive | Yes (root) | SCSI power management |
| **sg_ses** | ses | Yes (root) | SES LED control |
| **lsblk** | identify, config | Yes | Block device info |
| **zpool** | zfs, identify | Optional | ZFS pool status |
| **zfs** | identify | Optional | ZFS dataset/vdev GUIDs |
| **storcli** | hba | Optional | LSI/Broadcom HBA |
| **sas3ircu** | hba | Optional | SAS3008 HBA |
| **lvdisplay/vgdisplay/pvdisplay** | identify | Optional | LVM info |
| **mdadm** | identify | Optional | MD RAID info |

---

## Gaps / Incomplete Areas

| Area | Status | Notes |
|------|--------|-------|
| **Alert delivery** | ⚠️ Stubbed | Framework exists, no webhook/email calls |
| **NVMe support** | ❌ Excluded | Filtered out in discovery |
| **USB drives** | ❌ Excluded | Filtered out in discovery |
| **LVM management** | ⚠️ Identify only | No health monitoring |
| **MD RAID management** | ⚠️ Identify only | No health monitoring |

---

## Architectural Layers

The codebase has two distinct capability layers:

### 1. Enclosure-centric (Original Vision)
- HBA discovery (sas3ircu, storcli)
- SES LED control
- Enclosure:slot addressing
- JBOD-specific commands
- Physical drive location

### 2. Drive-centric (Expanded Scope)
- Universal identification
- Any block device support
- ZFS/LVM/MD awareness
- Works without enclosures
- Software RAID integration

---

## Key Implementation Patterns

### Concurrency
- Goroutines with WaitGroups for parallel drive queries
- Used in: status, healthcheck, identify index building

### Caching
- Multi-tier TTL system reduces external tool calls
- Global singleton cache with cleanup

### Error Handling
- Graceful fallbacks (identify without HBA, locate without SES)
- nil pointers for unavailable data (standby drives don't report temp)

### Output Formats
- JSON for machine consumption (`--json` flag)
- Table/text for human consumption
- Consistent across all commands

---

## Potential Roadmap Directions

### High Impact / High Accomplishability
1. **Complete alert delivery** - Webhook/email implementation (framework exists)
2. **NVMe support** - Remove exclusion, add nvme-cli integration
3. **Daemon mode** - Long-running service with HTTP API

### High Impact / Medium Accomplishability
4. **LVM health monitoring** - Extend beyond identification
5. **MD RAID health monitoring** - Extend beyond identification
6. **Multi-enclosure orchestration** - Better support for multiple JBODs

### Strategic Decisions
7. **Tool split** - Separate `jbodgod` (enclosures) from universal drive tool
8. **Plugin system** - Extensible storage backend support
9. **Prometheus metrics** - Monitoring integration
