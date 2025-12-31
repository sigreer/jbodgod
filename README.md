# JBODgod

JBOD and storage drive management tool for Linux. Manages SAS/SATA drives in JBOD enclosures with ZFS and LVM support.

## Features

- Drive status monitoring (state, temperature)
- Power management (spindown, spinup)
- JSON API output for integrations
- ZFS pool/vdev detection
- SCSI address and serial number reporting
- Real-time temperature monitoring
- Safe power-off sequence

## Installation

### From source (Go)

```bash
cd app
go build -o jbodgod ./cmd/jbodgod
sudo mv jbodgod /usr/local/bin/
```

### Shell script (legacy)

```bash
sudo cp scripts/drives.sh /usr/local/bin/drives
sudo chmod +x /usr/local/bin/drives
```

## Usage

```bash
# Show status
jbodgod status

# JSON output (for sidebars/integrations)
jbodgod json

# Monitor with 5s refresh
jbodgod monitor -i 5

# Spin down all drives
jbodgod spindown

# Spin up all drives
jbodgod spinup
```

## Configuration

Copy `config.example.yaml` to `/etc/jbodgod/config.yaml` or `~/.config/jbodgod/config.yaml`.

## Requirements

- `smartmontools` (smartctl)
- `sdparm`
- `lsscsi`
- ZFS utils (optional, for pool detection)

## License

MIT
