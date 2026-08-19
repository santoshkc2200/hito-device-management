package cliimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
)

// The devices.csv template documents the full column set (see
// cmd/hdms-cli/templates/devices.csv); only asset_tag, name and category
// are required here, enforced by readCSVRows.

const deviceDateLayout = "2006-01-02"

// DeviceImportDeps are the services ImportDevices orchestrates.
type DeviceImportDeps struct {
	Catalog     catalogapi.Service
	Credentials credentialsapi.Service
}

// ImportDevices reads devices.csv from r and, for each row, creates a new
// device or updates the existing one matched by asset tag (idempotent
// across repeated runs on the same file). With dryRun, no write is made —
// LookupDeviceByAssetTag alone (a read) decides the reported outcome.
func ImportDevices(ctx context.Context, deps DeviceImportDeps, r io.Reader, dryRun, mintCredentials bool) (Report, error) {
	rows, err := readCSVRows(r, []string{"asset_tag", "name", "category"})
	if err != nil {
		return Report{}, fmt.Errorf("cliimport: devices: %w", err)
	}

	report := Report{DryRun: dryRun}
	for i, row := range rows {
		line := i + 2 // header is line 1
		result := importDeviceRow(ctx, deps, line, row, dryRun, mintCredentials)
		report.Rows = append(report.Rows, result)
	}
	return report, nil
}

func importDeviceRow(ctx context.Context, deps DeviceImportDeps, line int, row map[string]string, dryRun, mintCredentials bool) RowResult {
	assetTag := row["asset_tag"]
	name := row["name"]
	category := row["category"]
	result := RowResult{Line: line, Ref: assetTag}

	if assetTag == "" || name == "" || category == "" {
		result.Outcome = OutcomeRejected
		result.Reason = "asset_tag, name and category are required"
		return result
	}

	var acquiredOn *time.Time
	if raw := row["acquired_on"]; raw != "" {
		t, err := time.Parse(deviceDateLayout, raw)
		if err != nil {
			result.Outcome = OutcomeRejected
			result.Reason = fmt.Sprintf("acquired_on: invalid date %q, expected YYYY-MM-DD", raw)
			return result
		}
		acquiredOn = &t
	}

	existing, err := deps.Catalog.LookupDeviceByAssetTag(ctx, assetTag)
	found := true
	if errors.Is(err, catalogapi.ErrDeviceNotFound) {
		found = false
	} else if err != nil {
		result.Outcome = OutcomeRejected
		result.Reason = fmt.Sprintf("lookup failed: %v", err)
		return result
	}

	if dryRun {
		if found {
			result.Outcome = OutcomeUpdated
		} else {
			result.Outcome = OutcomeCreated
		}
		return result
	}

	cat, err := deps.Catalog.GetOrCreateCategory(ctx, category)
	if err != nil {
		result.Outcome = OutcomeRejected
		result.Reason = fmt.Sprintf("resolve category %q: %v", category, err)
		return result
	}

	var deviceID string
	if found {
		updated, err := deps.Catalog.UpdateDevice(ctx, existing.ID, catalogapi.UpdateDeviceParams{
			Name:         name,
			CategoryID:   cat.ID,
			Manufacturer: row["manufacturer"],
			Model:        row["model"],
			SerialNo:     row["serial_no"],
			HomeLocation: row["home_location"],
			Notes:        row["notes"],
			AcquiredOn:   acquiredOn,
		}, "import")
		if err != nil {
			result.Outcome = OutcomeRejected
			result.Reason = fmt.Sprintf("update failed: %v", err)
			return result
		}
		deviceID = updated.ID
		result.Outcome = OutcomeUpdated
	} else {
		created, err := deps.Catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
			AssetTag:     assetTag,
			Name:         name,
			CategoryID:   cat.ID,
			Manufacturer: row["manufacturer"],
			Model:        row["model"],
			SerialNo:     row["serial_no"],
			HomeLocation: row["home_location"],
			Notes:        row["notes"],
			AcquiredOn:   acquiredOn,
		}, "import")
		if err != nil {
			result.Outcome = OutcomeRejected
			result.Reason = fmt.Sprintf("create failed: %v", err)
			return result
		}
		deviceID = created.ID
		result.Outcome = OutcomeCreated
	}

	if mintCredentials && result.Outcome == OutcomeCreated {
		issued, err := deps.Credentials.Issue(ctx, credentialsapi.IssueParams{
			SubjectType: credentialsapi.SubjectDevice,
			SubjectID:   deviceID,
			Kind:        credentialsapi.KindQR,
			IssuedBy:    "import",
		})
		if err != nil {
			result.Reason = fmt.Sprintf("device created but credential mint failed: %v", err)
		} else {
			result.CredentialToken = issued.Token
		}
	}

	return result
}
