package cliimport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
)

// The staff.csv template documents the full column set (see
// cmd/hdms-cli/templates/staff.csv); only employee_no and full_name are
// required here, enforced by readCSVRows.

// UserImportDeps are the services ImportUsers orchestrates.
type UserImportDeps struct {
	Identity    identityapi.Service
	Credentials credentialsapi.Service
}

// ImportUsers reads staff.csv from r and, for each row, registers a new
// user or updates the existing one matched by employee number (idempotent
// across repeated runs on the same file). Employee number itself is never
// editable — only the other fields are updated on a match, matching
// UpdateUserParams's own contract. With dryRun, no write is made —
// LookupUserByEmployeeNo alone (a read) decides the reported outcome.
func ImportUsers(ctx context.Context, deps UserImportDeps, r io.Reader, dryRun, mintCredentials bool) (Report, error) {
	rows, err := readCSVRows(r, []string{"employee_no", "full_name"})
	if err != nil {
		return Report{}, fmt.Errorf("cliimport: users: %w", err)
	}

	report := Report{DryRun: dryRun}
	for i, row := range rows {
		line := i + 2 // header is line 1
		result := importUserRow(ctx, deps, line, row, dryRun, mintCredentials)
		report.Rows = append(report.Rows, result)
	}
	return report, nil
}

func importUserRow(ctx context.Context, deps UserImportDeps, line int, row map[string]string, dryRun, mintCredentials bool) RowResult {
	employeeNo := row["employee_no"]
	fullName := row["full_name"]
	result := RowResult{Line: line, Ref: employeeNo}

	if employeeNo == "" || fullName == "" {
		result.Outcome = OutcomeRejected
		result.Reason = "employee_no and full_name are required"
		return result
	}

	existing, err := deps.Identity.LookupUserByEmployeeNo(ctx, employeeNo)
	found := true
	if errors.Is(err, identityapi.ErrUserNotFound) {
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

	var departmentID string
	if dept := row["department"]; dept != "" {
		d, err := deps.Identity.GetOrCreateDepartment(ctx, dept)
		if err != nil {
			result.Outcome = OutcomeRejected
			result.Reason = fmt.Sprintf("resolve department %q: %v", dept, err)
			return result
		}
		departmentID = d.ID
	}

	var userID string
	if found {
		updated, err := deps.Identity.UpdateUser(ctx, existing.ID, identityapi.UpdateUserParams{
			FullName:     fullName,
			DepartmentID: departmentID,
			Email:        row["email"],
			Phone:        row["phone"],
			Notes:        row["notes"],
		}, "import")
		if err != nil {
			result.Outcome = OutcomeRejected
			result.Reason = fmt.Sprintf("update failed: %v", err)
			return result
		}
		userID = updated.ID
		result.Outcome = OutcomeUpdated
	} else {
		created, err := deps.Identity.CreateUser(ctx, identityapi.CreateUserParams{
			EmployeeNo:   employeeNo,
			FullName:     fullName,
			DepartmentID: departmentID,
			Email:        row["email"],
			Phone:        row["phone"],
			Notes:        row["notes"],
			RegisteredBy: "import",
		})
		if err != nil {
			result.Outcome = OutcomeRejected
			result.Reason = fmt.Sprintf("create failed: %v", err)
			return result
		}
		userID = created.ID
		result.Outcome = OutcomeCreated
	}

	if mintCredentials && result.Outcome == OutcomeCreated {
		issued, err := deps.Credentials.Issue(ctx, credentialsapi.IssueParams{
			SubjectType: credentialsapi.SubjectUser,
			SubjectID:   userID,
			Kind:        credentialsapi.KindQR,
			IssuedBy:    "import",
		})
		if err != nil {
			result.Reason = fmt.Sprintf("user created but credential mint failed: %v", err)
		} else {
			result.CredentialToken = issued.Token
		}
	}

	return result
}
