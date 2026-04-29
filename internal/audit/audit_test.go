package audit_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/audit"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/model"
)

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:audit_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() { s, _ := db.DB(); s.Close() })
	return db
}

func TestLogWritesEntry(t *testing.T) {
	db := openDB(t)
	w := audit.NewWriter(db)

	op := &model.Operator{Base: model.Base{ID: 7}, Email: "x@y.com"}
	ctx := auth.WithOperator(context.Background(), op)

	require.NoError(t, w.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(42),
		Action:    "project.create",
		Target:    "projects/clinic-network",
		Extra:     map[string]any{"display_name": "Clinic Network"},
	}))

	var got model.AuditLog
	require.NoError(t, db.First(&got).Error)
	require.NotNil(t, got.OperatorID)
	require.Equal(t, uint64(7), *got.OperatorID)
	require.NotNil(t, got.ProjectID)
	require.Equal(t, uint64(42), *got.ProjectID)
	require.Equal(t, "project.create", got.Action)
	require.Equal(t, "projects/clinic-network", got.Target)
	require.Contains(t, got.ExtraJSON, "Clinic Network")
}

func TestLogAllowsNilOperator(t *testing.T) {
	db := openDB(t)
	w := audit.NewWriter(db)

	require.NoError(t, w.Log(context.Background(), audit.Entry{
		Action: "system.startup",
	}))

	var got model.AuditLog
	require.NoError(t, db.First(&got).Error)
	require.Nil(t, got.OperatorID)
	require.Equal(t, "system.startup", got.Action)
}

func TestLogPullsSourceIPFromContext(t *testing.T) {
	db := openDB(t)
	w := audit.NewWriter(db)

	op := &model.Operator{Base: model.Base{ID: 1}, Email: "y@z.com"}
	ctx := auth.WithOperator(context.Background(), op)
	ctx = audit.WithSourceIP(ctx, "203.0.113.42")

	require.NoError(t, w.Log(ctx, audit.Entry{Action: "x"}))

	var got model.AuditLog
	require.NoError(t, db.First(&got).Error)
	require.Equal(t, "203.0.113.42", got.SourceIP)
}

func ptrUint64(v uint64) *uint64 { return &v }
