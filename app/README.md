# JBODgod (Go)

Go implementation of the JBOD drive management CLI.

## Build

```bash
go build -o jbodgod ./cmd/jbodgod
```

## Install

```bash
sudo mv jbodgod /usr/local/bin/
```

## Usage

```bash
jbodgod status              # Show drive states and temperatures
jbodgod json                # JSON output for integrations
jbodgod monitor -i 5        # Live monitoring (5s refresh)
jbodgod spindown            # Spin down all drives
jbodgod spinup              # Spin up all drives
```

## Configuration

Uses config from (in order):
1. `--config` flag
2. `/etc/jbodgod/config.yaml`
3. `~/.config/jbodgod/config.yaml`
4. `./config.yaml`
5. Built-in defaults

## Project Structure

```
app/
├── cmd/jbodgod/main.go     # CLI entry point (cobra)
├── internal/
│   ├── config/             # YAML config handling
│   ├── drive/              # Drive operations (smartctl, sdparm)
│   ├── storage/            # ZFS/LVM management (planned)
│   ├── monitor/            # Real-time monitoring (planned)
│   └── alerts/             # Threshold alerts (planned)
├── go.mod
└── go.sum
```

## Dependencies

- `github.com/spf13/cobra` - CLI framework
- `gopkg.in/yaml.v3` - YAML config parsing
