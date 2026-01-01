package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// CreateAlert creates a new alert
func (d *DB) CreateAlert(alert *Alert) error {
	var detailsJSON sql.NullString
	if alert.Details != "" {
		detailsJSON = sql.NullString{String: alert.Details, Valid: true}
	}

	result, err := d.conn.Exec(`
		INSERT INTO alerts (severity, category, message, drive_serial, pool_name, enclosure_id, slot, details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, alert.Severity, alert.Category, alert.Message, nullString(alert.DriveSerial),
		nullString(alert.PoolName), alert.EnclosureID, alert.Slot, detailsJSON)
	if err != nil {
		return fmt.Errorf("failed to create alert: %w", err)
	}

	id, _ := result.LastInsertId()
	alert.ID = id
	alert.Timestamp = time.Now()

	return nil
}

// CreateAlertWithDetails creates a new alert with structured details
func (d *DB) CreateAlertWithDetails(severity, category, message string, details map[string]interface{}) error {
	var detailsJSON string
	if details != nil {
		b, err := json.Marshal(details)
		if err == nil {
			detailsJSON = string(b)
		}
	}

	alert := &Alert{
		Severity: severity,
		Category: category,
		Message:  message,
		Details:  detailsJSON,
	}

	// Extract common fields from details if present
	if details != nil {
		if serial, ok := details["serial"].(string); ok {
			alert.DriveSerial = serial
		}
		if pool, ok := details["pool"].(string); ok {
			alert.PoolName = pool
		}
		if enc, ok := details["enclosure"].(int); ok {
			alert.EnclosureID = &enc
		}
		if slot, ok := details["slot"].(int); ok {
			alert.Slot = &slot
		}
	}

	return d.CreateAlert(alert)
}

// GetUnacknowledgedAlerts returns all unacknowledged alerts
func (d *DB) GetUnacknowledgedAlerts() ([]*Alert, error) {
	rows, err := d.conn.Query(`
		SELECT id, severity, category, message, drive_serial, pool_name, enclosure_id, slot, details, acknowledged, ack_timestamp, timestamp
		FROM alerts
		WHERE acknowledged = 0
		ORDER BY timestamp DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query unacknowledged alerts: %w", err)
	}
	defer rows.Close()

	return scanAlerts(rows)
}

// GetAlerts returns alerts with optional filtering
func (d *DB) GetAlerts(severity string, limit int) ([]*Alert, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows *sql.Rows
	var err error

	if severity != "" {
		rows, err = d.conn.Query(`
			SELECT id, severity, category, message, drive_serial, pool_name, enclosure_id, slot, details, acknowledged, ack_timestamp, timestamp
			FROM alerts
			WHERE severity = ?
			ORDER BY timestamp DESC
			LIMIT ?
		`, severity, limit)
	} else {
		rows, err = d.conn.Query(`
			SELECT id, severity, category, message, drive_serial, pool_name, enclosure_id, slot, details, acknowledged, ack_timestamp, timestamp
			FROM alerts
			ORDER BY timestamp DESC
			LIMIT ?
		`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query alerts: %w", err)
	}
	defer rows.Close()

	return scanAlerts(rows)
}

// GetAlertsByCategory returns alerts of a specific category
func (d *DB) GetAlertsByCategory(category string, limit int) ([]*Alert, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := d.conn.Query(`
		SELECT id, severity, category, message, drive_serial, pool_name, enclosure_id, slot, details, acknowledged, ack_timestamp, timestamp
		FROM alerts
		WHERE category = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, category, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query alerts by category: %w", err)
	}
	defer rows.Close()

	return scanAlerts(rows)
}

// AcknowledgeAlert marks an alert as acknowledged
func (d *DB) AcknowledgeAlert(id int64) error {
	_, err := d.conn.Exec(`
		UPDATE alerts SET acknowledged = 1, ack_timestamp = ? WHERE id = ?
	`, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to acknowledge alert: %w", err)
	}
	return nil
}

// AcknowledgeAllAlerts marks all alerts as acknowledged
func (d *DB) AcknowledgeAllAlerts() (int64, error) {
	result, err := d.conn.Exec(`
		UPDATE alerts SET acknowledged = 1, ack_timestamp = ? WHERE acknowledged = 0
	`, time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to acknowledge all alerts: %w", err)
	}
	return result.RowsAffected()
}

// AlertCount returns counts of alerts by severity
func (d *DB) AlertCount() (total, unacked, critical, warning int, err error) {
	row := d.conn.QueryRow(`
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN acknowledged = 0 THEN 1 ELSE 0 END) as unacked,
			SUM(CASE WHEN severity = 'critical' AND acknowledged = 0 THEN 1 ELSE 0 END) as critical,
			SUM(CASE WHEN severity = 'warning' AND acknowledged = 0 THEN 1 ELSE 0 END) as warning
		FROM alerts
	`)
	err = row.Scan(&total, &unacked, &critical, &warning)
	return
}

// DeleteOldAlerts removes acknowledged alerts older than the given duration
func (d *DB) DeleteOldAlerts(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := d.conn.Exec(`
		DELETE FROM alerts WHERE acknowledged = 1 AND timestamp < ?
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old alerts: %w", err)
	}
	return result.RowsAffected()
}

func scanAlerts(rows *sql.Rows) ([]*Alert, error) {
	var alerts []*Alert
	for rows.Next() {
		var alert Alert
		var driveSerial, poolName, details sql.NullString
		var enclosureID, slot sql.NullInt64
		var ackTimestamp sql.NullTime
		var acknowledged int

		err := rows.Scan(
			&alert.ID, &alert.Severity, &alert.Category, &alert.Message,
			&driveSerial, &poolName, &enclosureID, &slot, &details,
			&acknowledged, &ackTimestamp, &alert.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}

		alert.DriveSerial = driveSerial.String
		alert.PoolName = poolName.String
		alert.Details = details.String
		alert.Acknowledged = acknowledged == 1
		if ackTimestamp.Valid {
			alert.AckTimestamp = &ackTimestamp.Time
		}
		if enclosureID.Valid {
			enc := int(enclosureID.Int64)
			alert.EnclosureID = &enc
		}
		if slot.Valid {
			sl := int(slot.Int64)
			alert.Slot = &sl
		}

		alerts = append(alerts, &alert)
	}

	return alerts, rows.Err()
}
