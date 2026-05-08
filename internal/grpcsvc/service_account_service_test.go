package grpcsvc_test

import (
	"context"
	"strings"
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

func newSAService(t *testing.T, db *gorm.DB) *grpcsvc.ServiceAccountService {
	t.Helper()
	return grpcsvc.NewServiceAccountService(
		dao.NewServiceAccountTokenDAO(db),
		dao.NewOperatorDAO(db),
		audit.NewWriter(db),
		zap.NewNop(),
	)
}

func TestSACreateRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	user := seedOperator(t, db, "user@x.com", "p", false)
	svc := newSAService(t, db)
	ctx := auth.WithOperator(context.Background(), user)

	_, err := svc.CreateServiceAccountToken(ctx, &quicktunv1.CreateServiceAccountTokenRequest{
		Operator: opName(user.ID),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestSACreateUnauthenticated(t *testing.T) {
	db := openTestDB(t)
	svc := newSAService(t, db)
	_, err := svc.CreateServiceAccountToken(context.Background(), &quicktunv1.CreateServiceAccountTokenRequest{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestSACreateReturnsRaw(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	target := seedOperator(t, db, "ci@x.com", "p", false)
	svc := newSAService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.CreateServiceAccountToken(ctx, &quicktunv1.CreateServiceAccountTokenRequest{
		Operator:    opName(target.ID),
		Description: "ci-deploy",
		TtlSeconds:  3600,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetRaw())
	require.True(t, strings.HasPrefix(resp.GetRaw(), "qt_sat_"))
	require.NotZero(t, resp.GetToken().GetId())
	require.Equal(t, opName(target.ID), resp.GetToken().GetOperator())
	require.Equal(t, "ci-deploy", resp.GetToken().GetDescription())
	require.NotNil(t, resp.GetToken().GetExpireTime())

	// Audit row written.
	var n int64
	require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "sa_token.create").Count(&n).Error)
	require.EqualValues(t, 1, n)
}

func TestSACreateMissingOperator(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	svc := newSAService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.CreateServiceAccountToken(ctx, &quicktunv1.CreateServiceAccountTokenRequest{
		Operator: "operators/99999",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestSAList(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	target := seedOperator(t, db, "ci@x.com", "p", false)
	svc := newSAService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	for _, desc := range []string{"first", "second"} {
		_, err := svc.CreateServiceAccountToken(ctx, &quicktunv1.CreateServiceAccountTokenRequest{
			Operator: opName(target.ID), Description: desc,
		})
		require.NoError(t, err)
	}

	resp, err := svc.ListServiceAccountTokens(ctx, &quicktunv1.ListServiceAccountTokensRequest{
		Operator: opName(target.ID),
	})
	require.NoError(t, err)
	require.Len(t, resp.GetTokens(), 2)
	require.Equal(t, "first", resp.GetTokens()[0].GetDescription())
	require.Equal(t, "second", resp.GetTokens()[1].GetDescription())
}

func TestSARevokeIdempotent(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	target := seedOperator(t, db, "ci@x.com", "p", false)
	svc := newSAService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	created, err := svc.CreateServiceAccountToken(ctx, &quicktunv1.CreateServiceAccountTokenRequest{
		Operator: opName(target.ID), Description: "x",
	})
	require.NoError(t, err)

	id := created.GetToken().GetId()
	_, err = svc.RevokeServiceAccountToken(ctx, &quicktunv1.RevokeServiceAccountTokenRequest{Id: id})
	require.NoError(t, err)

	// Token should now be marked revoked.
	rec, err := dao.NewServiceAccountTokenDAO(db).FindByID(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, rec.RevokedAt)

	// Second revoke is a no-op.
	_, err = svc.RevokeServiceAccountToken(ctx, &quicktunv1.RevokeServiceAccountTokenRequest{Id: id})
	require.NoError(t, err)

	// Revoking a missing id is also OK.
	_, err = svc.RevokeServiceAccountToken(ctx, &quicktunv1.RevokeServiceAccountTokenRequest{Id: 999_999})
	require.NoError(t, err)
}

func TestSARevokeRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	user := seedOperator(t, db, "u@x.com", "p", false)
	svc := newSAService(t, db)
	ctx := auth.WithOperator(context.Background(), user)

	_, err := svc.RevokeServiceAccountToken(ctx, &quicktunv1.RevokeServiceAccountTokenRequest{Id: 1})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}
