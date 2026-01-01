package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// RecordEvent logs a drive state transition event
func (d *DB) RecordEvent(driveID int64, eventType, oldState, newState, devicePath string, details map[string]interface{}) error {
	var detailsJSON string
	if details != nil {
		b, err := json.Marshal(details)
		if err == nil {
			detailsJSON = string(b)
		}
	}

	// Get current enclosure/slot from drive record
	var enclosureID, slot sql.NullInt64
	d.conn.QueryRow("SELECT enclosure_id, slot FROM drives WHERE id = ?", driveID).Scan(&enclosureID, &slot)

	_, err := d.conn.Exec(`
		INSERT INTO drive_events (drive_id, event_type, old_state, new_state, device_path, enclosure_id, slot, details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, driveID, eventType, oldState, newState, devicePath, enclosureID, slot, detailsJSON)

	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return nil
}

// GetDriveEvents returns events for a specific drive
func (d *DB) GetDriveEvents(driveID int64, limit int) ([]*DriveEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := d.conn.Query(`
		SELECT id, drive_id, event_type, old_state, new_state, device_path, enclosure_id, slot, details, timestamp
		FROM drive_events
		WHERE drive_id = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, driveID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query drive events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// GetDriveEventsBySerial returns events for a drive by serial number
func (d *DB) GetDriveEventsBySerial(serial string, limit int) ([]*DriveEvent, error) {
	drive, err := d.GetDriveBySerial(serial)
	if err != nil {
		return nil, err
	}
	if drive == nil {
		return nil, nil
	}

	return d.GetDriveEvents(drive.ID, limit)
}

// GetRecentEvents returns the most recent events across all drives
func (d *DB) GetRecentEvents(limit int) ([]*DriveEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := d.conn.Query(`
		SELECT id, drive_id, event_type, old_state, new_state, device_path, enclosure_id, slot, details, timestamp
		FROM drive_events
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// GetEventsSince returns events since a given timestamp
func (d *DB) GetEventsSince(since time.Time) ([]*DriveEvent, error) {
	rows, err := d.conn.Query(`
		SELECT id, drive_id, event_type, old_state, new_state, device_path, enclosure_id, slot, details, timestamp
		FROM drive_events
		WHERE timestamp > ?
		ORDER BY timestamp DESC
	`, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query events since: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// GetEventsByType returns events of a specific type
func (d *DB) GetEventsByType(eventType string, limit int) ([]*DriveEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := d.conn.Query(`
		SELECT id, drive_id, event_type, old_state, new_state, device_path, enclosure_id, slot, details, timestamp
		FROM drive_events
		WHERE event_type = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, eventType, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query events by type: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]*DriveEvent, error) {
	var events []*DriveEvent
	for rows.Next() {
		var event DriveEvent
		var enclosureID, slot sql.NullInt64
		var devicePath, oldState, newState, details sql.NullString

		err := rows.Scan(
			&event.ID, &event.DriveID, &event.EventType,
			&oldState, &newState, &devicePath,
			&enclosureID, &slot, &details, &event.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		event.OldState = oldState.String
		event.NewState = newState.String
		event.DevicePath = devicePath.String
		event.Details = details.String
		if enclosureID.Valid {
			enc := int(enclosureID.Int64)
			event.EnclosureID = &enc
		}
		if slot.Valid {
			sl := int(slot.Int64)
			event.Slot = &sl
		}

		events = append(events, &event)
	}

	return events, rows.Err()
}
