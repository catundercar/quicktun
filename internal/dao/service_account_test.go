package dao_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

func mkOperator(t *testing.T, db *gorm.DB, email string) *model.Operator {
	t.Helper()
	op, err := dao.NewOperatorDAO(db).Create(context.Background(), email, "h", false)
	require.NoError(t, err)
	return op
}

func TestSATokenIssueAndValidate(t *testing.T) {
	db := openWithModels(t)
	op := mkOperator(t, db, "ci@x.com")
	store := dao.NewServiceAccountTokenDAO(db)
	ctx := context.Background()

	rec, raw, err := store.Issue(ctx, op.ID, "ci-deploy", 24*time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.True(t, strings.HasPrefix(raw, dao.SATokenPrefix), "raw must carry the qt_sat_ prefix: %q", raw)
	require.NotEmpty(t, rec.TokenHash)
	require.Equal(t, "ci-deploy", rec.Description)
	require.NotNil(t, rec.ExpiresAt)
	require.WithinDuration(t, time.Now().Add(24*time.Hour), *rec.ExpiresAt, 5*time.Second)

	gotID, err := store.ValidateRaw(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, op.ID, gotID)

	// Validate must have bumped last_used_at.
	got, err := store.FindByID(ctx, rec.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
}

func TestSATokenIssueWithoutTTLNeverExpires(t *testing.T) {
	db := openWithModels(t)
	op := mkOperator(t, db, "ci@x.com")
	store := dao.NewServiceAccountTokenDAO(db)
	ctx := context.Background()

	rec, raw, err := store.Issue(ctx, op.ID, "perm", 0)
	require.NoError(t, err)
	require.Nil(t, rec.ExpiresAt)

	gotID, err := store.ValidateRaw(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, op.ID, gotID)
}

func TestSATokenValidateRejectsUnknown(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewServiceAccountTokenDAO(db)
	_, err := store.ValidateRaw(context.Background(), "qt_sat_bogus")
	require.Error(t, err)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestSATokenValidateRejectsExpired(t *testing.T) {
	db := openWithModels(t)
	op := mkOperator(t, db, "ci@x.com")
	store := dao.NewServiceAccountTokenDAO(db)
	ctx := context.Background()

	rec, raw, err := store.Issue(ctx, op.ID, "short", time.Hour)
	require.NoError(t, err)

	// Force expiry into the past.
	past := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, db.Model(&model.ServiceAccountToken{}).
		Where("id = ?", rec.ID).
		Update("expires_at", past).Error)

	_, err = store.ValidateRaw(ctx, raw)
	require.Error(t, err)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestSATokenValidateRejectsRevoked(t *testing.T) {
	db := openWithModels(t)
	op := mkOperator(t, db, "ci@x.com")
	store := dao.NewServiceAccountTokenDAO(db)
	ctx := context.Background()

	rec, raw, err := store.Issue(ctx, op.ID, "rev", time.Hour)
	require.NoError(t, err)
	require.NoError(t, store.Revoke(ctx, rec.ID))

	_, err = store.ValidateRaw(ctx, raw)
	require.Error(t, err)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestSATokenRevokeIsIdempotent(t *testing.T) {
	db := openWithModels(t)
	op := mkOperator(t, db, "ci@x.com")
	store := dao.NewServiceAccountTokenDAO(db)
	ctx := context.Background()

	rec, _, err := store.Issue(ctx, op.ID, "x", time.Hour)
	require.NoError(t, err)

	require.NoError(t, store.Revoke(ctx, rec.ID))
	// Second revoke is a no-op (no error).
	require.NoError(t, store.Revoke(ctx, rec.ID))
	// Revoking a missing id is a no-op.
	require.NoError(t, store.Revoke(ctx, 999_999))
}

func TestSATokenListByOperator(t *testing.T) {
	db := openWithModels(t)
	op1 := mkOperator(t, db, "a@x.com")
	op2 := mkOperator(t, db, "b@x.com")
	store := dao.NewServiceAccountTokenDAO(db)
	ctx := context.Background()

	_, _, err := store.Issue(ctx, op1.ID, "first", time.Hour)
	require.NoError(t, err)
	_, _, err = store.Issue(ctx, op1.ID, "second", 0)
	require.NoError(t, err)
	_, _, err = store.Issue(ctx, op2.ID, "other", time.Hour)
	require.NoError(t, err)

	rows, err := store.ListByOperator(ctx, op1.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "first", rows[0].Description)
	require.Equal(t, "second", rows[1].Description)

	rows, err = store.ListByOperator(ctx, op2.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "other", rows[0].Description)
}
