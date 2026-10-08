package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"LLMGateway/server/internal/db/sqlc"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/store"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type queryStage struct {
	name string
	row  []any
	rows [][]any
	tag  string
	err  error
}

type stagedDB struct {
	pgx.Tx
	stages []queryStage
}

func (db *stagedDB) next(query string) queryStage {
	if len(db.stages) == 0 {
		return queryStage{err: fmt.Errorf("unexpected query: %s", query)}
	}
	stage := db.stages[0]
	db.stages = db.stages[1:]
	if !strings.Contains(query, "-- name: "+stage.name+" ") {
		return queryStage{err: fmt.Errorf("expected query %s, got %s", stage.name, query)}
	}
	return stage
}

type stagedRow struct {
	values []any
	err    error
}

func (r stagedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(r.values) != len(dest) {
		return fmt.Errorf("fixture has %d columns, scan needs %d", len(r.values), len(dest))
	}
	for i, value := range r.values {
		field := reflect.ValueOf(dest[i]).Elem()
		if value == nil {
			field.Set(reflect.Zero(field.Type()))
			continue
		}
		v := reflect.ValueOf(value)
		if !v.Type().AssignableTo(field.Type()) {
			return fmt.Errorf("fixture column %d has %T, want %v", i, value, field.Type())
		}
		field.Set(v)
	}
	return nil
}

type stagedRows struct {
	pgx.Rows
	values [][]any
	index  int
}

func (r *stagedRows) Next() bool { r.index++; return r.index <= len(r.values) }
func (r *stagedRows) Scan(dest ...any) error {
	return (stagedRow{values: r.values[r.index-1]}).Scan(dest...)
}
func (*stagedRows) Err() error { return nil }
func (*stagedRows) Close()     {}

func (db *stagedDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	stage := db.next(query)
	return stagedRow{stage.row, stage.err}
}
func (db *stagedDB) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	stage := db.next(query)
	if stage.err != nil {
		return nil, stage.err
	}
	return &stagedRows{values: stage.rows}, nil
}
func (db *stagedDB) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	stage := db.next(query)
	tag := stage.tag
	if tag == "" {
		tag = "UPDATE 1"
	}
	return pgconn.NewCommandTag(tag), stage.err
}

func TestQuotaTransitionsAbortAtFailedPersistenceStage(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	items := queryStage{name: "LockQuotaReservationItems", rows: [][]any{{int64(1), pgtype.Timestamptz{Time: now, Valid: true}, int64(5), "0.500000"}}}
	pending := queryStage{name: "LockQuotaReservationStatus", row: []any{"pending"}}
	identity := queryStage{name: "LockQuotaReservationIdentity", row: []any{"pending", "request", int64(1), int64(2)}}
	cases := []struct {
		name   string
		stages []queryStage
		call   func(*Tx) error
		want   error
	}{
		{"release item query", []queryStage{pending, {name: "LockQuotaReservationItems", err: errDatabaseFault}}, func(tx *Tx) error { return tx.ReleaseReservation(1, "released", now) }, errDatabaseFault},
		{"release bucket query", []queryStage{pending, items, {name: "ReleaseQuotaBucket", err: errDatabaseFault}}, func(tx *Tx) error { return tx.ReleaseReservation(1, "released", now) }, errDatabaseFault},
		{"release bucket underflow", []queryStage{pending, items, {name: "ReleaseQuotaBucket", tag: "UPDATE 0"}}, func(tx *Tx) error { return tx.ReleaseReservation(1, "released", now) }, store.ErrInvalid},
		{"settle item query", []queryStage{identity, {name: "LockQuotaReservationItems", err: errDatabaseFault}}, func(tx *Tx) error { return tx.SettleQuotaReservation(1, "request", 1, 2, 3, "0.300000") }, errDatabaseFault},
		{"settle bucket query", []queryStage{identity, items, {name: "SettleQuotaBucket", err: errDatabaseFault}}, func(tx *Tx) error { return tx.SettleQuotaReservation(1, "request", 1, 2, 3, "0.300000") }, errDatabaseFault},
		{"settle bucket underflow", []queryStage{identity, items, {name: "SettleQuotaBucket", tag: "UPDATE 0"}}, func(tx *Tx) error { return tx.SettleQuotaReservation(1, "request", 1, 2, 3, "0.300000") }, store.ErrInvalid},
		{"settle item update", []queryStage{identity, items, {name: "SettleQuotaBucket"}, {name: "SettleQuotaReservationItem", err: errDatabaseFault}}, func(tx *Tx) error { return tx.SettleQuotaReservation(1, "request", 1, 2, 3, "0.300000") }, errDatabaseFault},
		{"cleanup identity query", []queryStage{{name: "LockQuotaPoliciesForCleanup"}, {name: "LockQuotaReservationsForCleanup", err: errDatabaseFault}}, func(tx *Tx) error { return tx.DeleteQuotaReservationsForUser(1) }, errDatabaseFault},
		{"cleanup release", []queryStage{{name: "LockQuotaPoliciesForCleanup"}, {name: "LockQuotaReservationsForCleanup", rows: [][]any{{int64(1)}}}, {name: "LockQuotaReservationStatus", err: errDatabaseFault}}, func(tx *Tx) error { return tx.DeleteQuotaReservationsForUser(1) }, errDatabaseFault},
		{"reap release", []queryStage{{name: "LockExpiredQuotaReservations", rows: [][]any{{int64(1)}}}, {name: "LockQuotaReservationStatus", err: errDatabaseFault}}, func(tx *Tx) error { _, e := tx.ReapExpired(now, 10, 1, 2); return e }, errDatabaseFault},
		{"key cleanup", []queryStage{{name: "LockQuotaPoliciesForCleanup"}, {name: "LockQuotaReservationsForCleanup"}, {name: "DeleteKeyQuotaReservations"}}, func(tx *Tx) error { return tx.DeleteQuotaReservationsForKey(1, 2) }, nil},
		{"user cleanup", []queryStage{{name: "LockQuotaPoliciesForCleanup"}, {name: "LockQuotaReservationsForCleanup"}, {name: "DeleteUserQuotaReservations"}}, func(tx *Tx) error { return tx.DeleteQuotaReservationsForUser(1) }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := &stagedDB{stages: append([]queryStage(nil), tc.stages...)}
			tx := &Tx{ctx: context.Background(), tx: db, queries: sqlc.New(db), now: func() time.Time { return now }}
			if e := tc.call(tx); !errors.Is(e, tc.want) {
				t.Fatalf("error = %v, want %v", e, tc.want)
			}
			if len(db.stages) != 0 {
				t.Fatalf("expected failure stage not reached: %+v", db.stages)
			}
		})
	}
}

func TestCorruptQuotaScopeCannotBecomeUnownedData(t *testing.T) {
	ctx := context.Background()
	// Explicit NULL identities simulate corrupt persisted rows. The ownership
	// adapter must reject them rather than returning scope id zero to callers.
	get := &Store{queries: sqlc.New(&faultDB{failAt: 99})}
	if _, e := get.GetQuotaPolicy(ctx, 1, 1); !errors.Is(e, store.ErrInvalid) {
		t.Fatalf("policy error = %v", e)
	}
	policyRow := []any{int64(1), "corrupt", "user", pgtype.Int8{}, pgtype.Int8{}, "day", pgtype.Int8{}, nil, true}
	list := &Store{queries: sqlc.New(&stagedDB{stages: []queryStage{{name: "ListQuotaPolicies", rows: [][]any{policyRow}}}})}
	if _, e := list.ListQuotaPolicies(ctx, quota.QuotaPolicyFilter{OwnerUserID: 1}); !errors.Is(e, store.ErrInvalid) {
		t.Fatalf("policy list error = %v", e)
	}
	usageRow := []any{int64(1), "corrupt", "user", pgtype.Int8{}, pgtype.Int8{}, "day", pgtype.Timestamptz{}, pgtype.Timestamptz{}, pgtype.Int8{}, int64(0), int64(0), "", "0.000000", "0.000000"}
	list = &Store{queries: sqlc.New(&stagedDB{stages: []queryStage{{name: "ListQuotaUsage", rows: [][]any{usageRow}}}})}
	if _, e := list.ListQuotaUsage(ctx, quota.QuotaPolicyFilter{OwnerUserID: 1}); !errors.Is(e, store.ErrInvalid) {
		t.Fatalf("quota usage error = %v", e)
	}
}

func TestQuotaFinalizationRejectsInvalidIdentityAndTerminalState(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	for _, status := range []string{"released", "settled", "expired"} {
		t.Run("release "+status, func(t *testing.T) {
			db := &stagedDB{stages: []queryStage{{name: "LockQuotaReservationStatus", row: []any{status}}}}
			if e := releaseQuotaTx(context.Background(), db, 1, "released", now); e != nil || len(db.stages) != 0 {
				t.Fatalf("terminal release must be idempotent: %v, stages=%d", e, len(db.stages))
			}
		})
	}
	for _, tc := range []struct {
		name, status, request string
		userID, keyID         int64
	}{
		{"settled", "settled", "request", 1, 2},
		{"released", "released", "request", 1, 2},
		{"request mismatch", "pending", "other", 1, 2},
		{"user mismatch", "pending", "request", 9, 2},
		{"key mismatch", "pending", "request", 1, 9},
	} {
		t.Run("settle "+tc.name, func(t *testing.T) {
			db := &stagedDB{stages: []queryStage{{name: "LockQuotaReservationIdentity", row: []any{tc.status, tc.request, tc.userID, tc.keyID}}}}
			if e := settleQuotaTx(context.Background(), db, 1, "request", 1, 2, 5, "0.500000", now); !errors.Is(e, store.ErrInvalid) || len(db.stages) != 0 {
				t.Fatalf("invalid settlement = %v, stages=%d", e, len(db.stages))
			}
		})
	}
	if e := settleQuotaTx(context.Background(), &stagedDB{}, 0, "request", 1, 2, 5, "0.500000", now); e != nil {
		t.Fatalf("no quota reservation must need no persistence: %v", e)
	}
}
