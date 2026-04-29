package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/model"
)

func TestAuditLogCreate(t *testing.T) {
	db := openMemDB(t)

	opID := uint64(1)
	pID := uint64(7)
	entry := model.AuditLog{
		Ts:         time.Now().UTC(),
		ProjectID:  &pID,
		OperatorID: &opID,
		Action:     "site.create",
		Target:     "projects/p1/sites/bastion-01",
		SourceIP:   "203.0.113.7",
		ExtraJSON:  `{"foo":"bar"}`,
	}
	require.NoError(t, db.Create(&entry).Error)
	require.NotZero(t, entry.ID)
}

func TestAuditLogAllowsNullActor(t *testing.T) {
	db := openMemDB(t)

	entry := model.AuditLog{
		Ts:     time.Now().UTC(),
		Action: "system.startup",
	}
	require.NoError(t, db.Create(&entry).Error)
}

func TestAuditLogHasNoSoftDelete(t *testing.T) {
	db := openMemDB(t)

	entry := model.AuditLog{
		Ts:     time.Now().UTC(),
		Action: "test.event",
	}
	require.NoError(t, db.Create(&entry).Error)

	// Hard delete should remove the row entirely (no soft-delete behavior).
	require.NoError(t, db.Delete(&entry).Error)

	var count int64
	require.NoError(t, db.Model(&model.AuditLog{}).Count(&count).Error)
	require.Equal(t, int64(0), count)
}
