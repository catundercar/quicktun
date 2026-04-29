package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/model"
)

func openMemDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Per-test named in-memory DB. The "name" portion (t.Name()) makes each
	// test instance get its own database; ?cache=shared lets the gorm pool's
	// connections all hit the same in-memory DB. t.Cleanup closes it so the
	// memory is released and -count=N reruns get a fresh DB each iteration.
	dsn := "file:qttest_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})
	return db
}

func TestOperatorRoundTrip(t *testing.T) {
	db := openMemDB(t)

	op := model.Operator{
		Email:        "alice@example.com",
		PasswordHash: "$2a$12$...",
		IsAdmin:      true,
	}
	require.NoError(t, db.Create(&op).Error)
	require.NotZero(t, op.ID)

	var got model.Operator
	require.NoError(t, db.First(&got, op.ID).Error)
	require.Equal(t, "alice@example.com", got.Email)
	require.True(t, got.IsAdmin)
}

func TestOperatorEmailUnique(t *testing.T) {
	db := openMemDB(t)
	require.NoError(t, db.Create(&model.Operator{Email: "a@x.com", PasswordHash: "x"}).Error)
	err := db.Create(&model.Operator{Email: "a@x.com", PasswordHash: "y"}).Error
	require.Error(t, err)
}

func TestOperatorEmailReusableAfterSoftDelete(t *testing.T) {
	db := openMemDB(t)

	op := model.Operator{Email: "reuse@x.com", PasswordHash: "x"}
	require.NoError(t, db.Create(&op).Error)
	require.NoError(t, db.Delete(&op).Error) // soft delete

	op2 := model.Operator{Email: "reuse@x.com", PasswordHash: "y"}
	require.NoError(t, db.Create(&op2).Error)
	require.NotEqual(t, op.ID, op2.ID)
}

func TestOperatorSessionExpires(t *testing.T) {
	db := openMemDB(t)
	op := model.Operator{Email: "b@x.com", PasswordHash: "x"}
	require.NoError(t, db.Create(&op).Error)

	now := time.Now().UTC()
	sess := model.OperatorSession{
		OperatorID: op.ID,
		TokenHash:  "deadbeef",
		IssuedAt:   now,
		ExpiresAt:  now.Add(8 * time.Hour),
		UserAgent:  "quicktun-cli/0.1",
		SourceIP:   "203.0.113.7",
	}
	require.NoError(t, db.Create(&sess).Error)
	require.NotZero(t, sess.ID)
}
