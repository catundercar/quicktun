package grpcsvc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/grpcsvc"
	"github.com/tulip/quicktun/internal/model"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:auth_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() { s, _ := db.DB(); s.Close() })
	return db
}

func seedOperator(t *testing.T, db *gorm.DB, email, password string, admin bool) *model.Operator {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	op, err := dao.NewOperatorDAO(db).Create(context.Background(), email, string(hash), admin)
	require.NoError(t, err)
	return op
}

func newAuthService(t *testing.T, db *gorm.DB) *grpcsvc.AuthService {
	return grpcsvc.NewAuthService(dao.NewOperatorDAO(db), dao.NewSessionDAO(db), 8*time.Hour)
}

func TestLoginSuccess(t *testing.T) {
	db := openTestDB(t)
	seedOperator(t, db, "alice@example.com", "hunter2", true)
	svc := newAuthService(t, db)

	resp, err := svc.Login(context.Background(), &quicktunv1.LoginRequest{
		Email: "alice@example.com", Password: "hunter2",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.AccessToken)
	require.NotNil(t, resp.ExpireTime)
	require.Equal(t, "alice@example.com", resp.Operator.Email)
	require.True(t, resp.Operator.IsAdmin)
}

func TestLoginWrongPassword(t *testing.T) {
	db := openTestDB(t)
	seedOperator(t, db, "alice@example.com", "hunter2", false)
	svc := newAuthService(t, db)

	_, err := svc.Login(context.Background(), &quicktunv1.LoginRequest{
		Email: "alice@example.com", Password: "WRONG",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestLoginUnknownEmail(t *testing.T) {
	db := openTestDB(t)
	svc := newAuthService(t, db)

	_, err := svc.Login(context.Background(), &quicktunv1.LoginRequest{
		Email: "nobody@example.com", Password: "anything",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestLoginRejectsEmptyFields(t *testing.T) {
	db := openTestDB(t)
	svc := newAuthService(t, db)

	_, err := svc.Login(context.Background(), &quicktunv1.LoginRequest{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestWhoAmIReturnsCallerOperator(t *testing.T) {
	db := openTestDB(t)
	op := seedOperator(t, db, "wa@x.com", "h", false)
	svc := newAuthService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	resp, err := svc.WhoAmI(ctx, &quicktunv1.WhoAmIRequest{})
	require.NoError(t, err)
	require.Equal(t, "wa@x.com", resp.Operator.Email)
}

func TestLogoutRevokesSession(t *testing.T) {
	db := openTestDB(t)
	seedOperator(t, db, "lo@x.com", "h", false)
	svc := newAuthService(t, db)

	loginResp, err := svc.Login(context.Background(), &quicktunv1.LoginRequest{Email: "lo@x.com", Password: "h"})
	require.NoError(t, err)

	// Simulate the interceptor having put the operator on context.
	op, err := dao.NewSessionDAO(db).Validate(context.Background(), loginResp.AccessToken)
	require.NoError(t, err)
	ctx := auth.WithOperator(context.Background(), op)
	// Provide raw token via context so service knows what to revoke.
	ctx = auth.WithRawToken(ctx, loginResp.AccessToken)

	_, err = svc.Logout(ctx, &quicktunv1.LogoutRequest{})
	require.NoError(t, err)

	// Token should no longer validate.
	_, err = dao.NewSessionDAO(db).Validate(context.Background(), loginResp.AccessToken)
	require.Error(t, err)
}

func TestLogoutWithoutTokenIsNoop(t *testing.T) {
	db := openTestDB(t)
	op := seedOperator(t, db, "no@x.com", "h", false)
	svc := newAuthService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	_, err := svc.Logout(ctx, &quicktunv1.LogoutRequest{})
	// Without a raw token in context, RevokeByToken is a no-op; this should
	// still succeed (idempotent).
	require.NoError(t, err)
	_ = (*emptypb.Empty)(nil) // silence unused import lint when emptypb omitted
}
