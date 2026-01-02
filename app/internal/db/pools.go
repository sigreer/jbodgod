package db

import (
	"encoding/json"
	"time"
)

// RecordPoolExport records that a pool was exported for spindown
func (d *DB) RecordPoolExport(poolName string, driveSerials []string, reason string) error {
	drivesJSON, err := json.Marshal(driveSerials)
	if err != nil {
		return err
	}

	_, err = d.conn.Exec(`
		INSERT INTO exported_pools (pool_name, export_reason, drives_json)
		VALUES (?, ?, ?)
	`, poolName, reason, string(drivesJSON))
	return err
}

// GetPendingImports returns all pools that need to be re-imported
func (d *DB) GetPendingImports() ([]*ExportedPool, error) {
	rows, err := d.conn.Query(`
		SELECT id, pool_name, export_timestamp, export_reason, drives_json
		FROM exported_pools
		WHERE imported_timestamp IS NULL
		ORDER BY export_timestamp ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pools []*ExportedPool
	for rows.Next() {
		p := &ExportedPool{}
		err := rows.Scan(&p.ID, &p.PoolName, &p.ExportTimestamp, &p.ExportReason, &p.DrivesJSON)
		if err != nil {
			return nil, err
		}
		pools = append(pools, p)
	}
	return pools, rows.Err()
}

// GetPendingImportsForDrives returns pools needing import that involve specific drives
func (d *DB) GetPendingImportsForDrives(driveSerials []string) ([]*ExportedPool, error) {
	if len(driveSerials) == 0 {
		return nil, nil
	}

	// Get all pending pools
	allPending, err := d.GetPendingImports()
	if err != nil {
		return nil, err
	}

	// Build a set of the target serials
	targetSet := make(map[string]bool)
	for _, s := range driveSerials {
		targetSet[s] = true
	}

	// Filter to pools that have at least one matching drive
	var matching []*ExportedPool
	for _, p := range allPending {
		var poolSerials []string
		if err := json.Unmarshal([]byte(p.DrivesJSON), &poolSerials); err != nil {
			continue
		}

		for _, s := range poolSerials {
			if targetSet[s] {
				matching = append(matching, p)
				break
			}
		}
	}

	return matching, nil
}

// MarkPoolImported updates a pool record as imported
func (d *DB) MarkPoolImported(poolName string, status string) error {
	_, err := d.conn.Exec(`
		UPDATE exported_pools
		SET imported_timestamp = ?, import_status = ?
		WHERE pool_name = ? AND imported_timestamp IS NULL
	`, time.Now(), status, poolName)
	return err
}

// ClearExportedPool removes all export records for a pool (for cleanup)
func (d *DB) ClearExportedPool(poolName string) error {
	_, err := d.conn.Exec(`DELETE FROM exported_pools WHERE pool_name = ?`, poolName)
	return err
}

// GetDriveSerials parses the drives_json field and returns the serial numbers
func (p *ExportedPool) GetDriveSerials() []string {
	var serials []string
	json.Unmarshal([]byte(p.DrivesJSON), &serials)
	return serials
}
