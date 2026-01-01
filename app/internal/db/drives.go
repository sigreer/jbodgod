package db

import (
	"database/sql"
	"fmt"
	"time"
)

// UpsertDrive inserts or updates a drive record
func (d *DB) UpsertDrive(drive *DriveRecord) error {
	now := time.Now()

	result, err := d.conn.Exec(`
		INSERT INTO drives (
			serial, serial_vpd, model, manufacturer, firmware, size_bytes,
			protocol, drive_type, enclosure_id, slot, sas_address, controller_id,
			device_path, wwn, luid, zpool_name, vdev_type, zfs_vdev_guid,
			current_state, first_seen, last_seen
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(serial) DO UPDATE SET
			serial_vpd = excluded.serial_vpd,
			model = COALESCE(excluded.model, model),
			manufacturer = COALESCE(excluded.manufacturer, manufacturer),
			firmware = COALESCE(excluded.firmware, firmware),
			size_bytes = COALESCE(excluded.size_bytes, size_bytes),
			protocol = COALESCE(excluded.protocol, protocol),
			drive_type = COALESCE(excluded.drive_type, drive_type),
			enclosure_id = COALESCE(excluded.enclosure_id, enclosure_id),
			slot = COALESCE(excluded.slot, slot),
			sas_address = COALESCE(excluded.sas_address, sas_address),
			controller_id = COALESCE(excluded.controller_id, controller_id),
			device_path = COALESCE(excluded.device_path, device_path),
			wwn = COALESCE(excluded.wwn, wwn),
			luid = COALESCE(excluded.luid, luid),
			zpool_name = COALESCE(excluded.zpool_name, zpool_name),
			vdev_type = COALESCE(excluded.vdev_type, vdev_type),
			zfs_vdev_guid = COALESCE(excluded.zfs_vdev_guid, zfs_vdev_guid),
			current_state = excluded.current_state,
			last_seen = excluded.last_seen
	`,
		drive.Serial, drive.SerialVPD, nullString(drive.Model), nullString(drive.Manufacturer),
		nullString(drive.Firmware), nullInt64(drive.SizeBytes), nullString(drive.Protocol),
		nullString(drive.DriveType), drive.EnclosureID, drive.Slot, nullString(drive.SASAddress),
		nullString(drive.ControllerID), nullString(drive.DevicePath), nullString(drive.WWN),
		nullString(drive.LUID), nullString(drive.ZpoolName), nullString(drive.VdevType),
		nullString(drive.ZFSVdevGUID), drive.CurrentState, now, now,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert drive: %w", err)
	}

	// Get the ID (either from insert or existing record)
	if drive.ID == 0 {
		id, err := result.LastInsertId()
		if err == nil && id > 0 {
			drive.ID = id
		} else {
			// Was an update, get existing ID
			existing, _ := d.GetDriveBySerial(drive.Serial)
			if existing != nil {
				drive.ID = existing.ID
			}
		}
	}

	return nil
}

// GetDriveBySerial returns a drive by its serial number
func (d *DB) GetDriveBySerial(serial string) (*DriveRecord, error) {
	row := d.conn.QueryRow(`
		SELECT id, serial, serial_vpd, model, manufacturer, firmware, size_bytes,
			protocol, drive_type, enclosure_id, slot, sas_address, controller_id,
			device_path, wwn, luid, zpool_name, vdev_type, zfs_vdev_guid,
			current_state, first_seen, last_seen
		FROM drives WHERE serial = ?
	`, serial)

	return scanDriveRow(row)
}

// GetDriveByLocation returns a drive by enclosure and slot
func (d *DB) GetDriveByLocation(enclosure, slot int) (*DriveRecord, error) {
	row := d.conn.QueryRow(`
		SELECT id, serial, serial_vpd, model, manufacturer, firmware, size_bytes,
			protocol, drive_type, enclosure_id, slot, sas_address, controller_id,
			device_path, wwn, luid, zpool_name, vdev_type, zfs_vdev_guid,
			current_state, first_seen, last_seen
		FROM drives WHERE enclosure_id = ? AND slot = ?
		ORDER BY last_seen DESC LIMIT 1
	`, enclosure, slot)

	return scanDriveRow(row)
}

// GetDriveByDevicePath returns a drive by its device path
func (d *DB) GetDriveByDevicePath(path string) (*DriveRecord, error) {
	row := d.conn.QueryRow(`
		SELECT id, serial, serial_vpd, model, manufacturer, firmware, size_bytes,
			protocol, drive_type, enclosure_id, slot, sas_address, controller_id,
			device_path, wwn, luid, zpool_name, vdev_type, zfs_vdev_guid,
			current_state, first_seen, last_seen
		FROM drives WHERE device_path = ?
	`, path)

	return scanDriveRow(row)
}

// GetAllDrives returns all known drives
func (d *DB) GetAllDrives() ([]*DriveRecord, error) {
	rows, err := d.conn.Query(`
		SELECT id, serial, serial_vpd, model, manufacturer, firmware, size_bytes,
			protocol, drive_type, enclosure_id, slot, sas_address, controller_id,
			device_path, wwn, luid, zpool_name, vdev_type, zfs_vdev_guid,
			current_state, first_seen, last_seen
		FROM drives ORDER BY enclosure_id, slot
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query drives: %w", err)
	}
	defer rows.Close()

	var drives []*DriveRecord
	for rows.Next() {
		drive, err := scanDriveRows(rows)
		if err != nil {
			return nil, err
		}
		drives = append(drives, drive)
	}

	return drives, rows.Err()
}

// GetDrivesByPool returns drives belonging to a ZFS pool
func (d *DB) GetDrivesByPool(poolName string) ([]*DriveRecord, error) {
	rows, err := d.conn.Query(`
		SELECT id, serial, serial_vpd, model, manufacturer, firmware, size_bytes,
			protocol, drive_type, enclosure_id, slot, sas_address, controller_id,
			device_path, wwn, luid, zpool_name, vdev_type, zfs_vdev_guid,
			current_state, first_seen, last_seen
		FROM drives WHERE zpool_name = ?
		ORDER BY enclosure_id, slot
	`, poolName)
	if err != nil {
		return nil, fmt.Errorf("failed to query drives by pool: %w", err)
	}
	defer rows.Close()

	var drives []*DriveRecord
	for rows.Next() {
		drive, err := scanDriveRows(rows)
		if err != nil {
			return nil, err
		}
		drives = append(drives, drive)
	}

	return drives, rows.Err()
}

// GetDrivesByState returns drives with a specific state
func (d *DB) GetDrivesByState(state string) ([]*DriveRecord, error) {
	rows, err := d.conn.Query(`
		SELECT id, serial, serial_vpd, model, manufacturer, firmware, size_bytes,
			protocol, drive_type, enclosure_id, slot, sas_address, controller_id,
			device_path, wwn, luid, zpool_name, vdev_type, zfs_vdev_guid,
			current_state, first_seen, last_seen
		FROM drives WHERE current_state = ?
		ORDER BY last_seen DESC
	`, state)
	if err != nil {
		return nil, fmt.Errorf("failed to query drives by state: %w", err)
	}
	defer rows.Close()

	var drives []*DriveRecord
	for rows.Next() {
		drive, err := scanDriveRows(rows)
		if err != nil {
			return nil, err
		}
		drives = append(drives, drive)
	}

	return drives, rows.Err()
}

// UpdateDriveState updates a drive's state and optionally records an event
func (d *DB) UpdateDriveState(serial, newState string, recordEvent bool) error {
	drive, err := d.GetDriveBySerial(serial)
	if err != nil {
		return err
	}

	oldState := drive.CurrentState

	_, err = d.conn.Exec(`
		UPDATE drives SET current_state = ?, last_seen = ? WHERE serial = ?
	`, newState, time.Now(), serial)
	if err != nil {
		return fmt.Errorf("failed to update drive state: %w", err)
	}

	if recordEvent && oldState != newState {
		return d.RecordEvent(drive.ID, eventTypeForStateChange(oldState, newState), oldState, newState, "", nil)
	}

	return nil
}

// DriveCount returns statistics about drives
func (d *DB) DriveCount() (total, active, missing, failed int, err error) {
	row := d.conn.QueryRow(`
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN current_state = 'active' THEN 1 ELSE 0 END) as active,
			SUM(CASE WHEN current_state = 'missing' THEN 1 ELSE 0 END) as missing,
			SUM(CASE WHEN current_state = 'failed' THEN 1 ELSE 0 END) as failed
		FROM drives
	`)
	err = row.Scan(&total, &active, &missing, &failed)
	return
}

// scanDriveRow scans a single row into a DriveRecord
func scanDriveRow(row *sql.Row) (*DriveRecord, error) {
	var drive DriveRecord
	var serialVPD, model, manufacturer, firmware, protocol, driveType sql.NullString
	var sasAddress, controllerID, devicePath, wwn, luid sql.NullString
	var zpoolName, vdevType, zfsVdevGUID sql.NullString
	var sizeBytes sql.NullInt64
	var enclosureID, slot sql.NullInt64

	err := row.Scan(
		&drive.ID, &drive.Serial, &serialVPD, &model, &manufacturer, &firmware, &sizeBytes,
		&protocol, &driveType, &enclosureID, &slot, &sasAddress, &controllerID,
		&devicePath, &wwn, &luid, &zpoolName, &vdevType, &zfsVdevGUID,
		&drive.CurrentState, &drive.FirstSeen, &drive.LastSeen,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan drive: %w", err)
	}

	drive.SerialVPD = serialVPD.String
	drive.Model = model.String
	drive.Manufacturer = manufacturer.String
	drive.Firmware = firmware.String
	drive.SizeBytes = sizeBytes.Int64
	drive.Protocol = protocol.String
	drive.DriveType = driveType.String
	if enclosureID.Valid {
		enc := int(enclosureID.Int64)
		drive.EnclosureID = &enc
	}
	if slot.Valid {
		sl := int(slot.Int64)
		drive.Slot = &sl
	}
	drive.SASAddress = sasAddress.String
	drive.ControllerID = controllerID.String
	drive.DevicePath = devicePath.String
	drive.WWN = wwn.String
	drive.LUID = luid.String
	drive.ZpoolName = zpoolName.String
	drive.VdevType = vdevType.String
	drive.ZFSVdevGUID = zfsVdevGUID.String

	return &drive, nil
}

// scanDriveRows scans a row from Rows into a DriveRecord
func scanDriveRows(rows *sql.Rows) (*DriveRecord, error) {
	var drive DriveRecord
	var serialVPD, model, manufacturer, firmware, protocol, driveType sql.NullString
	var sasAddress, controllerID, devicePath, wwn, luid sql.NullString
	var zpoolName, vdevType, zfsVdevGUID sql.NullString
	var sizeBytes sql.NullInt64
	var enclosureID, slot sql.NullInt64

	err := rows.Scan(
		&drive.ID, &drive.Serial, &serialVPD, &model, &manufacturer, &firmware, &sizeBytes,
		&protocol, &driveType, &enclosureID, &slot, &sasAddress, &controllerID,
		&devicePath, &wwn, &luid, &zpoolName, &vdevType, &zfsVdevGUID,
		&drive.CurrentState, &drive.FirstSeen, &drive.LastSeen,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan drive row: %w", err)
	}

	drive.SerialVPD = serialVPD.String
	drive.Model = model.String
	drive.Manufacturer = manufacturer.String
	drive.Firmware = firmware.String
	drive.SizeBytes = sizeBytes.Int64
	drive.Protocol = protocol.String
	drive.DriveType = driveType.String
	if enclosureID.Valid {
		enc := int(enclosureID.Int64)
		drive.EnclosureID = &enc
	}
	if slot.Valid {
		sl := int(slot.Int64)
		drive.Slot = &sl
	}
	drive.SASAddress = sasAddress.String
	drive.ControllerID = controllerID.String
	drive.DevicePath = devicePath.String
	drive.WWN = wwn.String
	drive.LUID = luid.String
	drive.ZpoolName = zpoolName.String
	drive.VdevType = vdevType.String
	drive.ZFSVdevGUID = zfsVdevGUID.String

	return &drive, nil
}

// Helper functions for nullable values
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt64(i int64) sql.NullInt64 {
	if i == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: i, Valid: true}
}

func eventTypeForStateChange(old, new string) string {
	switch new {
	case StateMissing:
		return EventMissing
	case StateFailed:
		return EventFailed
	case StateActive:
		if old == StateMissing || old == StateFailed {
			return EventOnline
		}
		return EventOnline
	case StateStandby:
		return EventOffline
	default:
		return "state_change"
	}
}
