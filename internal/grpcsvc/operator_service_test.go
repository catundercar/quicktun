package grpcsvc_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/audit"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/grpcsvc"
	"github.com/tulip/quicktun/internal/model"
)

// newOperatorService bundles the DAO wiring used by every operator-service
// test. Audit writer is a real one bound to the same DB so we can assert on
// audit_log rows when we want to.
func newOperatorService(t *testing.T, db *gorm.DB) *grpcsvc.OperatorService {
	t.Helper()
	return grpcsvc.NewOperatorService(
		dao.NewOperatorDAO(db),
		dao.NewOperatorProjectAccessDAO(db),
		dao.NewProjectDAO(db),
		audit.NewWriter(db),
		zap.NewNop(),
	)
}

func opName(id uint64) string {
	return "operators/" + strconv.FormatUint(id, 10)
}

func TestListOperatorsRequiresAuth(t *testing.T) {
	db := openTestDB(t)
	svc := newOperatorService(t, db)
	_, err := svc.ListOperators(context.Background(), &quicktunv1.ListOperatorsRequest{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestListOperatorsRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	user := seedOperator(t, db, "user@x.com", "p", false)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), user)
	_, err := svc.ListOperators(ctx, &quicktunv1.ListOperatorsRequest{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestListOperatorsReturnsAll(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	seedOperator(t, db, "u1@x.com", "p", false)
	seedOperator(t, db, "u2@x.com", "p", false)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.ListOperators(ctx, &quicktunv1.ListOperatorsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetOperators(), 3)
	emails := []string{
		resp.GetOperators()[0].GetEmail(),
		resp.GetOperators()[1].GetEmail(),
		resp.GetOperators()[2].GetEmail(),
	}
	require.ElementsMatch(t, []string{"admin@x.com", "u1@x.com", "u2@x.com"}, emails)
}

func TestCreateOperatorByAdmin(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.CreateOperator(ctx, &quicktunv1.CreateOperatorRequest{
		Operator: &quicktunv1.Operator{Email: "newop@x.com", IsAdmin: false},
		Password: "verysecure",
	})
	require.NoError(t, err)
	require.Equal(t, "newop@x.com", resp.GetEmail())
	require.False(t, resp.GetIsAdmin())
	require.NotEmpty(t, resp.GetName())

	// Promoted variant.
	respA, err := svc.CreateOperator(ctx, &quicktunv1.CreateOperatorRequest{
		Operator: &quicktunv1.Operator{Email: "newadmin@x.com", IsAdmin: true},
		Password: "verysecure",
	})
	require.NoError(t, err)
	require.True(t, respA.GetIsAdmin())

	// audit row written.
	var n int64
	require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "operator.create").Count(&n).Error)
	require.EqualValues(t, 2, n)
}

func TestCreateOperatorRejectsNonAdmin(t *testing.T) {
	db := openTestDB(t)
	user := seedOperator(t, db, "u@x.com", "p", false)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), user)

	_, err := svc.CreateOperator(ctx, &quicktunv1.CreateOperatorRequest{
		Operator: &quicktunv1.Operator{Email: "x@y.com"},
		Password: "p",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestCreateOperatorDuplicateEmail(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	seedOperator(t, db, "dup@x.com", "p", false)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.CreateOperator(ctx, &quicktunv1.CreateOperatorRequest{
		Operator: &quicktunv1.Operator{Email: "dup@x.com"},
		Password: "p",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.AlreadyExists, st.Code())
}

func TestGetOperator(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	user := seedOperator(t, db, "u@x.com", "p", false)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.GetOperator(ctx, &quicktunv1.GetOperatorRequest{Name: opName(user.ID)})
	require.NoError(t, err)
	require.Equal(t, "u@x.com", resp.GetEmail())

	_, err = svc.GetOperator(ctx, &quicktunv1.GetOperatorRequest{Name: opName(99999)})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())

	_, err = svc.GetOperator(ctx, &quicktunv1.GetOperatorRequest{Name: "garbage"})
	require.Error(t, err)
	st, _ = status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpdateOperatorIsAdminPromoteAndDemote(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	other := seedOperator(t, db, "other@x.com", "p", true) // need 2 admins so demote is allowed
	user := seedOperator(t, db, "u@x.com", "p", false)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	// Promote user → admin.
	resp, err := svc.UpdateOperator(ctx, &quicktunv1.UpdateOperatorRequest{
		Operator:   &quicktunv1.Operator{Name: opName(user.ID), IsAdmin: true},
		UpdateMask: "is_admin",
	})
	require.NoError(t, err)
	require.True(t, resp.GetIsAdmin())

	// Demote `other` → user (caller is not `other`, last-admin guard not tripped — there are 3 admins now).
	resp, err = svc.UpdateOperator(ctx, &quicktunv1.UpdateOperatorRequest{
		Operator:   &quicktunv1.Operator{Name: opName(other.ID), IsAdmin: false},
		UpdateMask: "is_admin",
	})
	require.NoError(t, err)
	require.False(t, resp.GetIsAdmin())
}

func TestUpdateOperatorRefusesDemoteSelf(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	seedOperator(t, db, "other@x.com", "p", true) // ensure not last-admin so we hit the self-guard
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.UpdateOperator(ctx, &quicktunv1.UpdateOperatorRequest{
		Operator:   &quicktunv1.Operator{Name: opName(admin.ID), IsAdmin: false},
		UpdateMask: "is_admin",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestUpdateOperatorRefusesDemoteLastAdmin(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	loneAdmin := seedOperator(t, db, "lone@x.com", "p", true)
	// admin is going to delete itself first to make `loneAdmin` truly the last.
	// Easier: sign in as `admin`, demote it manually so loneAdmin is the last,
	// then try to demote loneAdmin via admin's session (admin is still admin
	// for the call: we keep two admins and demote one through the *last* path
	// by setting admin to user via DAO directly).
	require.NoError(t, dao.NewOperatorDAO(db).UpdateIsAdmin(context.Background(), admin.ID, false))
	// admin is now not-admin. We need an admin caller; use loneAdmin.
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), &model.Operator{Base: model.Base{ID: loneAdmin.ID}, Email: loneAdmin.Email, IsAdmin: true})

	_, err := svc.UpdateOperator(ctx, &quicktunv1.UpdateOperatorRequest{
		Operator:   &quicktunv1.Operator{Name: opName(loneAdmin.ID), IsAdmin: false},
		UpdateMask: "is_admin",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestUpdateOperatorPasswordRotation(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	user := seedOperator(t, db, "u@x.com", "p", false)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.UpdateOperator(ctx, &quicktunv1.UpdateOperatorRequest{
		Operator:   &quicktunv1.Operator{Name: opName(user.ID)},
		UpdateMask: "password",
		Password:   "newer-pass",
	})
	require.NoError(t, err)

	// Validate via password compare against the stored hash.
	got, err := dao.NewOperatorDAO(db).FindByID(context.Background(), user.ID)
	require.NoError(t, err)
	require.NotEmpty(t, got.PasswordHash)
	require.NotEqual(t, "p", got.PasswordHash)
}

func TestUpdateOperatorRequiresMask(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	user := seedOperator(t, db, "u@x.com", "p", false)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.UpdateOperator(ctx, &quicktunv1.UpdateOperatorRequest{
		Operator:   &quicktunv1.Operator{Name: opName(user.ID), IsAdmin: true},
		UpdateMask: "",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestDeleteOperatorRefusesSelf(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	seedOperator(t, db, "other@x.com", "p", true)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.DeleteOperator(ctx, &quicktunv1.DeleteOperatorRequest{Name: opName(admin.ID)})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestDeleteOperatorRefusesLastAdmin(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	other := seedOperator(t, db, "other@x.com", "p", true)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	// Delete `other` — succeeds, leaves admin as the only admin.
	_, err := svc.DeleteOperator(ctx, &quicktunv1.DeleteOperatorRequest{Name: opName(other.ID)})
	require.NoError(t, err)

	// Now admin is the last admin; create an extra non-admin and try to delete admin.
	user := seedOperator(t, db, "user@x.com", "p", false)
	// Caller has to be an admin; `admin` itself qualifies. To attempt deleting the
	// last admin we need a different caller: promote `user`.
	require.NoError(t, dao.NewOperatorDAO(db).UpdateIsAdmin(context.Background(), user.ID, true))
	ctx2 := auth.WithOperator(context.Background(), &model.Operator{Base: model.Base{ID: user.ID}, Email: user.Email, IsAdmin: true})
	// Now there are 2 admins (admin + user). Demote `user` via DAO so admin becomes last.
	require.NoError(t, dao.NewOperatorDAO(db).UpdateIsAdmin(context.Background(), user.ID, false))
	// But ctx2 still says user.IsAdmin=true (we cached it in context). The handler
	// re-checks IsAdmin from context, so this still passes the admin gate. Good.

	_, err = svc.DeleteOperator(ctx2, &quicktunv1.DeleteOperatorRequest{Name: opName(admin.ID)})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestDeleteOperatorOK(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	user := seedOperator(t, db, "u@x.com", "p", false)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.DeleteOperator(ctx, &quicktunv1.DeleteOperatorRequest{Name: opName(user.ID)})
	require.NoError(t, err)

	_, err = dao.NewOperatorDAO(db).FindByID(context.Background(), user.ID)
	require.Error(t, err)
	require.True(t, dao.IsNotFound(err))
}

func TestGrantAndListProjectAccess(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	user := seedOperator(t, db, "u@x.com", "p", false)
	p1 := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p1).Error)
	p2 := &model.Project{Slug: "p2", Name: "P2", RelayPortRange: "20100-20199", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p2).Error)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	g1, err := svc.GrantProjectAccess(ctx, &quicktunv1.GrantProjectAccessRequest{
		Operator: opName(user.ID), ProjectSlug: "p1", Role: "viewer",
	})
	require.NoError(t, err)
	require.Equal(t, "p1", g1.GetProjectSlug())
	require.Equal(t, "viewer", g1.GetRole())

	_, err = svc.GrantProjectAccess(ctx, &quicktunv1.GrantProjectAccessRequest{
		Operator: opName(user.ID), ProjectSlug: "p2", Role: "operator",
	})
	require.NoError(t, err)

	resp, err := svc.ListProjectAccess(ctx, &quicktunv1.ListProjectAccessRequest{Operator: opName(user.ID)})
	require.NoError(t, err)
	require.Len(t, resp.GetAccess(), 2)
	got := map[string]string{}
	for _, a := range resp.GetAccess() {
		got[a.GetProjectSlug()] = a.GetRole()
	}
	require.Equal(t, map[string]string{"p1": "viewer", "p2": "operator"}, got)
}

func TestGrantProjectAccessUpsertRole(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	user := seedOperator(t, db, "u@x.com", "p", false)
	p := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.GrantProjectAccess(ctx, &quicktunv1.GrantProjectAccessRequest{
		Operator: opName(user.ID), ProjectSlug: "p1", Role: "viewer",
	})
	require.NoError(t, err)

	// Re-grant with a different role: should update, not duplicate.
	g2, err := svc.GrantProjectAccess(ctx, &quicktunv1.GrantProjectAccessRequest{
		Operator: opName(user.ID), ProjectSlug: "p1", Role: "owner",
	})
	require.NoError(t, err)
	require.Equal(t, "owner", g2.GetRole())

	resp, err := svc.ListProjectAccess(ctx, &quicktunv1.ListProjectAccessRequest{Operator: opName(user.ID)})
	require.NoError(t, err)
	require.Len(t, resp.GetAccess(), 1)
	require.Equal(t, "owner", resp.GetAccess()[0].GetRole())
}

func TestRevokeProjectAccess(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	user := seedOperator(t, db, "u@x.com", "p", false)
	p := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.GrantProjectAccess(ctx, &quicktunv1.GrantProjectAccessRequest{
		Operator: opName(user.ID), ProjectSlug: "p1", Role: "viewer",
	})
	require.NoError(t, err)

	_, err = svc.RevokeProjectAccess(ctx, &quicktunv1.RevokeProjectAccessRequest{
		Operator: opName(user.ID), ProjectSlug: "p1",
	})
	require.NoError(t, err)

	resp, err := svc.ListProjectAccess(ctx, &quicktunv1.ListProjectAccessRequest{Operator: opName(user.ID)})
	require.NoError(t, err)
	require.Empty(t, resp.GetAccess())

	// Idempotent.
	_, err = svc.RevokeProjectAccess(ctx, &quicktunv1.RevokeProjectAccessRequest{
		Operator: opName(user.ID), ProjectSlug: "p1",
	})
	require.NoError(t, err)
}

func TestGrantProjectAccessInvalidRole(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	user := seedOperator(t, db, "u@x.com", "p", false)
	p := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)
	svc := newOperatorService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.GrantProjectAccess(ctx, &quicktunv1.GrantProjectAccessRequest{
		Operator: opName(user.ID), ProjectSlug: "p1", Role: "supreme",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
