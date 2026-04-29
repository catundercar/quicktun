package dao_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/tulip/quicktun/internal/dao"
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
