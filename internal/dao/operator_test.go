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

func TestOperatorListAndPagination(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := ops.Create(ctx, "u"+string(rune('a'+i))+"@x.com", "h", i == 0)
		require.NoError(t, err)
	}

	page1, err := ops.List(ctx, 2, "")
	require.NoError(t, err)
	require.Len(t, page1, 2)
	tok := dao.NextOperatorPageToken(page1)
	require.NotEmpty(t, tok)

	page2, err := ops.List(ctx, 2, tok)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotEqual(t, page1[0].ID, page2[0].ID)

	page3, err := ops.List(ctx, 2, dao.NextOperatorPageToken(page2))
	require.NoError(t, err)
	require.Len(t, page3, 1)
	require.Empty(t, dao.NextOperatorPageToken(nil))

	_, err = ops.List(ctx, 2, "not-a-number")
	require.Error(t, err)
	require.True(t, errors.Is(err, dao.ErrInvalidPageToken))
}

func TestOperatorUpdateIsAdmin(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "u1@x.com", "h", false)
	require.NoError(t, err)

	require.NoError(t, ops.UpdateIsAdmin(ctx, op.ID, true))
	got, err := ops.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.True(t, got.IsAdmin)

	require.NoError(t, ops.UpdateIsAdmin(ctx, op.ID, false))
	got, err = ops.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.False(t, got.IsAdmin)

	err = ops.UpdateIsAdmin(ctx, 99999, true)
	require.Error(t, err)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestOperatorUpdatePassword(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "pw@x.com", "old-hash", false)
	require.NoError(t, err)

	require.NoError(t, ops.UpdatePassword(ctx, op.ID, "new-hash"))
	got, err := ops.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, "new-hash", got.PasswordHash)

	err = ops.UpdatePassword(ctx, 99999, "x")
	require.Error(t, err)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestOperatorDelete(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "del@x.com", "h", false)
	require.NoError(t, err)

	require.NoError(t, ops.Delete(ctx, op.ID))
	_, err = ops.FindByID(ctx, op.ID)
	require.Error(t, err)
	require.True(t, dao.IsNotFound(err))

	// Idempotent: deleting an already-gone row returns nil.
	require.NoError(t, ops.Delete(ctx, op.ID))

	// Email is now reusable (soft-delete + partial unique index).
	_, err = ops.Create(ctx, "del@x.com", "h", false)
	require.NoError(t, err)
}

func TestOperatorCountAdmins(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	ctx := context.Background()

	n, err := ops.CountAdmins(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 0, n)

	_, err = ops.Create(ctx, "a@x.com", "h", true)
	require.NoError(t, err)
	_, err = ops.Create(ctx, "b@x.com", "h", true)
	require.NoError(t, err)
	_, err = ops.Create(ctx, "c@x.com", "h", false)
	require.NoError(t, err)

	n, err = ops.CountAdmins(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
}

func TestOperatorProjectAccessGrantAndList(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	access := dao.NewOperatorProjectAccessDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "g@x.com", "h", false)
	require.NoError(t, err)

	p1 := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p1).Error)
	p2 := &model.Project{Slug: "p2", Name: "P2", RelayPortRange: "20100-20199", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p2).Error)

	g1, err := access.Grant(ctx, op.ID, p1.ID, model.ProjectRoleViewer)
	require.NoError(t, err)
	require.Equal(t, model.ProjectRoleViewer, g1.Role)

	// Grant again with the same role: idempotent (same row).
	g1again, err := access.Grant(ctx, op.ID, p1.ID, model.ProjectRoleViewer)
	require.NoError(t, err)
	require.Equal(t, g1.ID, g1again.ID)

	// Grant a different role on the same (operator, project): updates row.
	g1upd, err := access.Grant(ctx, op.ID, p1.ID, model.ProjectRoleOperator)
	require.NoError(t, err)
	require.Equal(t, g1.ID, g1upd.ID)
	require.Equal(t, model.ProjectRoleOperator, g1upd.Role)

	// Second project, different role.
	_, err = access.Grant(ctx, op.ID, p2.ID, model.ProjectRoleOwner)
	require.NoError(t, err)
}

func TestOperatorProjectAccessGrantValidRoles(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	access := dao.NewOperatorProjectAccessDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "g2@x.com", "h", false)
	require.NoError(t, err)

	p1 := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p1).Error)
	p2 := &model.Project{Slug: "p2", Name: "P2", RelayPortRange: "20100-20199", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p2).Error)

	_, err = access.Grant(ctx, op.ID, p1.ID, model.ProjectRoleViewer)
	require.NoError(t, err)
	_, err = access.Grant(ctx, op.ID, p2.ID, model.ProjectRoleOwner)
	require.NoError(t, err)

	rows, err := access.List(ctx, op.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	// rows are ordered by id ASC; p1 first.
	require.Equal(t, "p1", rows[0].ProjectSlug)
	require.Equal(t, model.ProjectRoleViewer, rows[0].Role)
	require.Equal(t, "p2", rows[1].ProjectSlug)
	require.Equal(t, model.ProjectRoleOwner, rows[1].Role)
}

func TestOperatorProjectAccessRevoke(t *testing.T) {
	db := openWithModels(t)
	ops := dao.NewOperatorDAO(db)
	access := dao.NewOperatorProjectAccessDAO(db)
	ctx := context.Background()

	op, err := ops.Create(ctx, "r@x.com", "h", false)
	require.NoError(t, err)
	p := &model.Project{Slug: "rp", Name: "RP", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)

	_, err = access.Grant(ctx, op.ID, p.ID, model.ProjectRoleOperator)
	require.NoError(t, err)

	require.NoError(t, access.Revoke(ctx, op.ID, p.ID))
	rows, err := access.List(ctx, op.ID)
	require.NoError(t, err)
	require.Empty(t, rows)

	// Idempotent.
	require.NoError(t, access.Revoke(ctx, op.ID, p.ID))

	// Re-grant after revoke is allowed (soft-delete + partial unique index).
	_, err = access.Grant(ctx, op.ID, p.ID, model.ProjectRoleViewer)
	require.NoError(t, err)
}
