# JBODgod (Go)

Go implementation of the JBOD drive management CLI.

See [main README](../README.md) for full documentation.

## Quick Start

```bash
go build -o jbodgod ./cmd/jbodgod
sudo ./jbodgod status
```

## Structure

```
cmd/jbodgod/     # CLI commands
internal/        # Core packages (config, drive, hba, ses, zfs, db, cache, identify)
```
