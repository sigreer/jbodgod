package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DefaultPath is the default database location
const DefaultPath = "/var/lib/jbodgod/inventory.db"

// DB wraps the SQLite database connection
type DB struct {
	conn *sql.DB
	path string
}

// New opens or creates the SQLite database at the given path
func New(path string) (*DB, error) {
	if path == "" {
		path = DefaultPath
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys and WAL mode for better concurrency
	if _, err := conn.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to configure database: %w", err)
	}

	db := &DB{conn: conn, path: path}

	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	return d.conn.Close()
}

// Path returns the database file path
func (d *DB) Path() string {
	return d.path
}

// migrate runs the database schema migrations
func (d *DB) migrate() error {
	// Create schema version table
	_, err := d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}

	// Get current version
	var version int
	err = d.conn.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return err
	}

	// Run migrations
	migrations := []string{
		migrationV1,
		migrationV2,
	}

	for i, migration := range migrations {
		v := i + 1
		if v <= version {
			continue
		}

		tx, err := d.conn.Begin()
		if err != nil {
			return err
		}

		if _, err := tx.Exec(migration); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration v%d failed: %w", v, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", v); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// migrationV1 creates the initial schema
const migrationV1 = `
-- Drive inventory: permanent record of all drives seen
CREATE TABLE IF NOT EXISTS drives (
    id INTEGER PRIMARY KEY,
    serial TEXT UNIQUE NOT NULL,
    serial_vpd TEXT,
    model TEXT,
    manufacturer TEXT,
    firmware TEXT,
    size_bytes INTEGER,
    protocol TEXT,
    drive_type TEXT,

    -- Current/last-known location
    enclosure_id INTEGER,
    slot INTEGER,
    sas_address TEXT,
    controller_id TEXT,

    -- Last known OS device info
    device_path TEXT,
    wwn TEXT,
    luid TEXT,

    -- ZFS info (may be stale if device failed)
    zpool_name TEXT,
    vdev_type TEXT,
    zfs_vdev_guid TEXT,

    -- State tracking
    current_state TEXT DEFAULT 'unknown',

    -- Timestamps
    first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_drives_serial ON drives(serial);
CREATE INDEX IF NOT EXISTS idx_drives_location ON drives(enclosure_id, slot);
CREATE INDEX IF NOT EXISTS idx_drives_zpool ON drives(zpool_name);
CREATE INDEX IF NOT EXISTS idx_drives_state ON drives(current_state);

-- State transition history for auditing/debugging
CREATE TABLE IF NOT EXISTS drive_events (
    id INTEGER PRIMARY KEY,
    drive_id INTEGER NOT NULL REFERENCES drives(id),
    event_type TEXT NOT NULL,
    old_state TEXT,
    new_state TEXT,
    device_path TEXT,
    enclosure_id INTEGER,
    slot INTEGER,
    details TEXT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_events_drive ON drive_events(drive_id);
CREATE INDEX IF NOT EXISTS idx_events_time ON drive_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_type ON drive_events(event_type);

-- ZFS pool health snapshots
CREATE TABLE IF NOT EXISTS zfs_health (
    id INTEGER PRIMARY KEY,
    pool_name TEXT NOT NULL,
    pool_state TEXT NOT NULL,
    scan_state TEXT,
    scan_progress REAL,
    read_errors INTEGER DEFAULT 0,
    write_errors INTEGER DEFAULT 0,
    cksum_errors INTEGER DEFAULT 0,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_zfs_pool ON zfs_health(pool_name);
CREATE INDEX IF NOT EXISTS idx_zfs_time ON zfs_health(timestamp);

-- ZFS vdev states (per-device within pool snapshot)
CREATE TABLE IF NOT EXISTS zfs_vdev_states (
    id INTEGER PRIMARY KEY,
    health_id INTEGER REFERENCES zfs_health(id),
    device_path TEXT,
    vdev_name TEXT,
    vdev_type TEXT,
    state TEXT,
    read_errors INTEGER DEFAULT 0,
    write_errors INTEGER DEFAULT 0,
    cksum_errors INTEGER DEFAULT 0,
    slow_ios INTEGER DEFAULT 0,
    drive_serial TEXT
);

CREATE INDEX IF NOT EXISTS idx_vdev_health ON zfs_vdev_states(health_id);

-- Alert/notification history
CREATE TABLE IF NOT EXISTS alerts (
    id INTEGER PRIMARY KEY,
    severity TEXT NOT NULL,
    category TEXT NOT NULL,
    message TEXT NOT NULL,
    drive_serial TEXT,
    pool_name TEXT,
    enclosure_id INTEGER,
    slot INTEGER,
    details TEXT,
    acknowledged INTEGER DEFAULT 0,
    ack_timestamp TIMESTAMP,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_alerts_unacked ON alerts(acknowledged) WHERE acknowledged = 0;
CREATE INDEX IF NOT EXISTS idx_alerts_time ON alerts(timestamp);
CREATE INDEX IF NOT EXISTS idx_alerts_severity ON alerts(severity);
`

// DriveRecord represents a drive in the database
type DriveRecord struct {
	ID           int64
	Serial       string
	SerialVPD    string
	Model        string
	Manufacturer string
	Firmware     string
	SizeBytes    int64
	Protocol     string
	DriveType    string
	EnclosureID  *int
	Slot         *int
	SASAddress   string
	ControllerID string
	DevicePath   string
	WWN          string
	LUID         string
	ZpoolName    string
	VdevType     string
	ZFSVdevGUID  string
	CurrentState string
	FirstSeen    time.Time
	LastSeen     time.Time
}

// DriveEvent represents a state change event
type DriveEvent struct {
	ID          int64
	DriveID     int64
	EventType   string
	OldState    string
	NewState    string
	DevicePath  string
	EnclosureID *int
	Slot        *int
	Details     string
	Timestamp   time.Time
}

// Alert represents an alert record
type Alert struct {
	ID           int64
	Severity     string
	Category     string
	Message      string
	DriveSerial  string
	PoolName     string
	EnclosureID  *int
	Slot         *int
	Details      string
	Acknowledged bool
	AckTimestamp *time.Time
	Timestamp    time.Time
}

// Event types
const (
	EventDiscovered = "discovered"
	EventOnline     = "online"
	EventOffline    = "offline"
	EventMissing    = "missing"
	EventFailed     = "failed"
	EventReplaced   = "replaced"
	EventMoved      = "moved"
)

// Drive states
const (
	StateUnknown = "unknown"
	StateActive  = "active"
	StateStandby = "standby"
	StateMissing = "missing"
	StateFailed  = "failed"
)

// Alert severities
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Alert categories
const (
	CategoryDriveMissing  = "drive_missing"
	CategoryDriveFailed   = "drive_failed"
	CategoryPoolDegraded  = "pool_degraded"
	CategoryTemperature   = "temperature"
	CategoryDriveNew      = "drive_new"
)

// migrationV2 adds exported_pools table for spindown/spinup tracking
const migrationV2 = `
-- Track ZFS pools exported for spindown operations
CREATE TABLE IF NOT EXISTS exported_pools (
    id INTEGER PRIMARY KEY,
    pool_name TEXT NOT NULL,
    export_timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    export_reason TEXT DEFAULT 'spindown',
    drives_json TEXT,
    imported_timestamp TIMESTAMP,
    import_status TEXT
);

CREATE INDEX IF NOT EXISTS idx_exported_pools_name ON exported_pools(pool_name);
CREATE INDEX IF NOT EXISTS idx_exported_pools_pending ON exported_pools(imported_timestamp) WHERE imported_timestamp IS NULL;
`

// ExportedPool represents a pool that was exported for spindown
type ExportedPool struct {
	ID                int64
	PoolName          string
	ExportTimestamp   time.Time
	ExportReason      string
	DrivesJSON        string
	ImportedTimestamp *time.Time
	ImportStatus      string
}
