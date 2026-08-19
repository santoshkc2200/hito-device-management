// Package identity owns the users and departments tables (docs/03). It
// never imports another module — only checkout orchestrates across module
// boundaries (.golangci.yml's identity-isolation rule) — and depends on
// auditapi only for recording the mutation events every write here
// produces.
package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/internal/domain"
	"github.com/hito-hospital/hdms/internal/modules/identity/internal/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Service implements identityapi.Service against Postgres.
type Service struct {
	pool  *db.Pool
	audit auditapi.Recorder
}

// New constructs the identity service.
func New(pool *db.Pool, audit auditapi.Recorder) *Service {
	return &Service{pool: pool, audit: audit}
}

var _ identityapi.Service = (*Service)(nil)

const defaultPageLimit = 50

func (s *Service) CreateUser(ctx context.Context, params identityapi.CreateUserParams) (identityapi.UserSummary, error) {
	employeeNo, err := domain.ValidateEmployeeNo(params.EmployeeNo)
	if err != nil {
		return identityapi.UserSummary{}, err
	}
	fullName, err := domain.ValidateFullName(params.FullName)
	if err != nil {
		return identityapi.UserSummary{}, err
	}
	registeredBy, err := domain.ValidateRegisteredBy(params.RegisteredBy)
	if err != nil {
		return identityapi.UserSummary{}, err
	}
	deptID, err := pgtypeconv.NullUUID(params.DepartmentID)
	if err != nil {
		return identityapi.UserSummary{}, fmt.Errorf("identity: invalid department id: %w", err)
	}

	var summary identityapi.UserSummary
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := identitystore.New(db.Conn(ctx, s.pool))
		row, err := q.CreateUser(ctx, identitystore.CreateUserParams{
			ID:           pgtypeconv.NewUUID(),
			EmployeeNo:   employeeNo,
			FullName:     fullName,
			DepartmentID: deptID,
			Email:        pgtypeconv.Text(params.Email),
			Phone:        pgtypeconv.Text(params.Phone),
			Notes:        pgtypeconv.Text(params.Notes),
			RegisteredBy: registeredBy,
		})
		if err != nil {
			return translateUserErr(err)
		}
		summary = toUserSummary(row)

		return s.audit.Record(ctx, auditapi.Event{
			Actor:   registeredBy,
			Action:  "user.created",
			Subject: "user:" + summary.ID,
			Payload: map[string]any{"employeeNo": summary.EmployeeNo},
		})
	})
	if err != nil {
		return identityapi.UserSummary{}, err
	}
	return summary, nil
}

func (s *Service) LookupUser(ctx context.Context, id string) (identityapi.UserSummary, error) {
	pid, err := pgtypeconv.UUID(id)
	if err != nil {
		return identityapi.UserSummary{}, fmt.Errorf("identity: invalid user id: %w", err)
	}
	q := identitystore.New(db.Conn(ctx, s.pool))
	row, err := q.GetUserByID(ctx, pid)
	if err != nil {
		return identityapi.UserSummary{}, translateUserErr(err)
	}
	return toUserSummary(row), nil
}

func (s *Service) LookupUserByEmployeeNo(ctx context.Context, employeeNo string) (identityapi.UserSummary, error) {
	q := identitystore.New(db.Conn(ctx, s.pool))
	row, err := q.GetUserByEmployeeNo(ctx, employeeNo)
	if err != nil {
		return identityapi.UserSummary{}, translateUserErr(err)
	}
	return toUserSummary(row), nil
}

func (s *Service) ListUsers(ctx context.Context, params identityapi.ListUsersParams) (identityapi.ListUsersResult, error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = defaultPageLimit
	}

	cursorAt, cursorID, err := decodeUserCursor(params.Cursor)
	if err != nil {
		return identityapi.ListUsersResult{}, fmt.Errorf("identity: invalid cursor: %w", err)
	}
	deptID, err := pgtypeconv.NullUUID(params.DepartmentID)
	if err != nil {
		return identityapi.ListUsersResult{}, fmt.Errorf("identity: invalid department id: %w", err)
	}

	q := identitystore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListUsers(ctx, identitystore.ListUsersParams{
		Status:             nullUserStatus(params.Status),
		DepartmentID:       deptID,
		Query:              pgtypeconv.Text(params.Query),
		CursorRegisteredAt: cursorAt,
		CursorID:           cursorID,
		ResultLimit:        int32(limit) + 1,
	})
	if err != nil {
		return identityapi.ListUsersResult{}, fmt.Errorf("identity: list users: %w", err)
	}

	result := identityapi.ListUsersResult{}
	for i, row := range rows {
		if i == limit {
			last := rows[i-1]
			result.NextCursor = encodeUserCursor(pgtypeconv.Time(last.RegisteredAt), pgtypeconv.UUIDString(last.ID))
			break
		}
		result.Items = append(result.Items, toUserSummary(row))
	}
	return result, nil
}

func (s *Service) UpdateUser(ctx context.Context, id string, params identityapi.UpdateUserParams, actor string) (identityapi.UserSummary, error) {
	pid, err := pgtypeconv.UUID(id)
	if err != nil {
		return identityapi.UserSummary{}, fmt.Errorf("identity: invalid user id: %w", err)
	}
	fullName, err := domain.ValidateFullName(params.FullName)
	if err != nil {
		return identityapi.UserSummary{}, err
	}
	deptID, err := pgtypeconv.NullUUID(params.DepartmentID)
	if err != nil {
		return identityapi.UserSummary{}, fmt.Errorf("identity: invalid department id: %w", err)
	}

	var summary identityapi.UserSummary
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := identitystore.New(db.Conn(ctx, s.pool))
		row, err := q.UpdateUser(ctx, identitystore.UpdateUserParams{
			ID:           pid,
			FullName:     fullName,
			DepartmentID: deptID,
			Email:        pgtypeconv.Text(params.Email),
			Phone:        pgtypeconv.Text(params.Phone),
			Notes:        pgtypeconv.Text(params.Notes),
		})
		if err != nil {
			return translateUserErr(err)
		}
		summary = toUserSummary(row)
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "user.updated", Subject: "user:" + summary.ID,
		})
	})
	if err != nil {
		return identityapi.UserSummary{}, err
	}
	return summary, nil
}

func (s *Service) SuspendUser(ctx context.Context, id, reason, actor string) (identityapi.UserSummary, error) {
	return s.transition(ctx, id, domain.StatusSuspended, "user.suspended", reason, actor)
}

func (s *Service) ReactivateUser(ctx context.Context, id, actor string) (identityapi.UserSummary, error) {
	return s.transition(ctx, id, domain.StatusActive, "user.reactivated", "", actor)
}

func (s *Service) ArchiveUser(ctx context.Context, id, reason, actor string) (identityapi.UserSummary, error) {
	return s.transition(ctx, id, domain.StatusArchived, "user.archived", reason, actor)
}

func (s *Service) transition(ctx context.Context, id string, to domain.UserStatus, action, reason, actor string) (identityapi.UserSummary, error) {
	pid, err := pgtypeconv.UUID(id)
	if err != nil {
		return identityapi.UserSummary{}, fmt.Errorf("identity: invalid user id: %w", err)
	}

	var summary identityapi.UserSummary
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := identitystore.New(db.Conn(ctx, s.pool))
		current, err := q.GetUserByID(ctx, pid)
		if err != nil {
			return translateUserErr(err)
		}
		if err := domain.ValidateTransition(domain.UserStatus(current.Status), to); err != nil {
			return fmt.Errorf("%w: %v", identityapi.ErrIllegalTransition, err)
		}

		row, err := q.UpdateUserStatus(ctx, identitystore.UpdateUserStatusParams{ID: pid, Status: identitystore.UserStatus(to)})
		if err != nil {
			return translateUserErr(err)
		}
		summary = toUserSummary(row)

		payload := map[string]any{}
		if reason != "" {
			payload["reason"] = reason
		}
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: action, Subject: "user:" + summary.ID, Payload: payload,
		})
	})
	if err != nil {
		return identityapi.UserSummary{}, err
	}
	return summary, nil
}

func (s *Service) GetOrCreateDepartment(ctx context.Context, name string) (identityapi.Department, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return identityapi.Department{}, fmt.Errorf("identity: department name is required")
	}
	q := identitystore.New(db.Conn(ctx, s.pool))
	row, err := q.GetOrCreateDepartment(ctx, identitystore.GetOrCreateDepartmentParams{
		ID:   pgtypeconv.NewUUID(),
		Name: name,
	})
	if err != nil {
		return identityapi.Department{}, fmt.Errorf("identity: get or create department: %w", err)
	}
	return identityapi.Department{ID: pgtypeconv.UUIDString(row.ID), Name: row.Name}, nil
}

func (s *Service) LookupDepartment(ctx context.Context, id string) (identityapi.Department, error) {
	did, err := pgtypeconv.UUID(id)
	if err != nil {
		return identityapi.Department{}, fmt.Errorf("identity: invalid department id: %w", err)
	}
	q := identitystore.New(db.Conn(ctx, s.pool))
	row, err := q.GetDepartmentByID(ctx, did)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return identityapi.Department{}, identityapi.ErrDepartmentNotFound
		}
		return identityapi.Department{}, fmt.Errorf("identity: lookup department: %w", err)
	}
	return identityapi.Department{ID: pgtypeconv.UUIDString(row.ID), Name: row.Name}, nil
}

func (s *Service) ListDepartments(ctx context.Context) ([]identityapi.Department, error) {
	q := identitystore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListDepartments(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: list departments: %w", err)
	}
	depts := make([]identityapi.Department, 0, len(rows))
	for _, r := range rows {
		depts = append(depts, identityapi.Department{ID: pgtypeconv.UUIDString(r.ID), Name: r.Name})
	}
	return depts, nil
}

func toUserSummary(row identitystore.User) identityapi.UserSummary {
	return identityapi.UserSummary{
		ID:           pgtypeconv.UUIDString(row.ID),
		EmployeeNo:   row.EmployeeNo,
		FullName:     row.FullName,
		DepartmentID: pgtypeconv.UUIDString(row.DepartmentID),
		Email:        pgtypeconv.TextString(row.Email),
		Phone:        pgtypeconv.TextString(row.Phone),
		Status:       identityapi.UserStatus(row.Status),
		Notes:        pgtypeconv.TextString(row.Notes),
		RegisteredAt: pgtypeconv.Time(row.RegisteredAt),
		RegisteredBy: row.RegisteredBy,
		UpdatedAt:    pgtypeconv.Time(row.UpdatedAt),
	}
}

func nullUserStatus(status identityapi.UserStatus) identitystore.NullUserStatus {
	if status == "" {
		return identitystore.NullUserStatus{}
	}
	return identitystore.NullUserStatus{UserStatus: identitystore.UserStatus(status), Valid: true}
}

// encodeUserCursor and decodeUserCursor implement opaque cursor pagination
// over (registered_at, id) — the same ordering ListUsers sorts by, so a
// cursor always resumes exactly where the previous page ended even as new
// rows are registered concurrently.
func encodeUserCursor(at time.Time, id string) string {
	raw := at.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeUserCursor(cursor string) (pgtype.Timestamptz, pgtype.UUID, error) {
	if cursor == "" {
		return pgtype.Timestamptz{}, pgtype.UUID{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return pgtype.Timestamptz{}, pgtype.UUID{}, fmt.Errorf("malformed cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, err
	}
	id, err := pgtypeconv.UUID(parts[1])
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, err
	}
	return pgtypeconv.Timestamptz(at), id, nil
}

func translateUserErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identityapi.ErrUserNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
		return identityapi.ErrEmployeeNoTaken
	}
	return err
}
