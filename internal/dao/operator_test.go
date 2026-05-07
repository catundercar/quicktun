package dao_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

func TestOperatorCreateAndFindByEmail(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewOperatorDAO(db)
	ctx := context.Background()

	hash, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.DefaultCost)
	require.NoError(t, err)

	op, err := store.Create(ctx, "alice@example.com", string(hash), false)
	require.NoError(t, err)
	require.NotZero(t, op.ID)

	got, err := store.FindByEmail(ctx, "alice@example.com")
	require.NoError(t, err)
	require.Equal(t, op.ID, got.ID)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("hunter2")))
}

func TestFindByEmailReturnsNotFound(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewOperatorDAO(db)
	_, err := store.FindByEmail(context.Background(), "nobody@x.com")
	require.Error(t, err)
	require.True(t, dao.IsNotFound(err))
}

func TestSessionIssueAndValidate(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	sess := dao.NewSessionDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "b@x.com", "h", false)
	require.NoError(t, err)

	rec, raw, err := sess.Issue(ctx, op.ID, 8*time.Hour, "test/1.0", "203.0.113.1")
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.NotEmpty(t, rec.TokenHash)
	require.WithinDuration(t, time.Now().Add(8*time.Hour), rec.ExpiresAt, 5*time.Second)

	gotOp, err := sess.Validate(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, op.ID, gotOp.ID)
}

func TestSessionValidateRejectsExpired(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	sess := dao.NewSessionDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "c@x.com", "h", false)
	require.NoError(t, err)

	// Issue with a tiny TTL, then wait it out.
	_, raw, err := sess.Issue(ctx, op.ID, 10*time.Millisecond, "ua", "ip")
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	_, err = sess.Validate(ctx, raw)
	require.Error(t, err)
}

func TestSessionRevoke(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	sess := dao.NewSessionDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "d@x.com", "h", false)
	require.NoError(t, err)
	rec, raw, err := sess.Issue(ctx, op.ID, time.Hour, "ua", "ip")
	require.NoError(t, err)

	require.NoError(t, sess.Revoke(ctx, rec.ID))

	_, err = sess.Validate(ctx, raw)
	require.Error(t, err)
}

func TestValidateSessionRaw(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	sess := dao.NewSessionDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "vsr@x.com", "h", false)
	require.NoError(t, err)
	_, raw, err := sess.Issue(ctx, op.ID, time.Hour, "ua", "ip")
	require.NoError(t, err)

	gotID, gotIsAdmin, err := sess.ValidateSessionRaw(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, op.ID, gotID)
	require.False(t, gotIsAdmin)
}

func TestValidateSessionRawRejectsInvalid(t *testing.T) {
	db := openWithModels(t)
	sess := dao.NewSessionDAO(db)

	_, _, err := sess.ValidateSessionRaw(context.Background(), "not-a-real-token")
	require.Error(t, err)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound),
		"expected wrapped ErrRecordNotFound, got: %v", err)
}

func TestValidateSessionRawRejectsExpired(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	sess := dao.NewSessionDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "exp@x.com", "h", false)
	require.NoError(t, err)
	_, raw, err := sess.Issue(ctx, op.ID, time.Hour, "ua", "ip")
	require.NoError(t, err)

	// Force expiry into the past directly in the DB.
	past := time.Now().UTC().Add(-1 * time.Hour)
	require.NoError(t, db.Model(&model.OperatorSession{}).
		Where("operator_id = ?", op.ID).
		Update("expires_at", past).Error)

	_, _, err = sess.ValidateSessionRaw(ctx, raw)
	require.Error(t, err)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound),
		"expected wrapped ErrRecordNotFound, got: %v", err)
}

func TestValidateSessionRawRejectsRevoked(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	sess := dao.NewSessionDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "rev@x.com", "h", false)
	require.NoError(t, err)
	rec, raw, err := sess.Issue(ctx, op.ID, time.Hour, "ua", "ip")
	require.NoError(t, err)
	require.NoError(t, sess.Revoke(ctx, rec.ID))

	_, _, err = sess.ValidateSessionRaw(ctx, raw)
	require.Error(t, err)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound),
		"expected wrapped ErrRecordNotFound, got: %v", err)
}
