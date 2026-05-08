package grpcsvc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/grpcsvc"
	"github.com/tulip/quicktun/internal/model"
)

// seedAuditEntries inserts n audit rows directly into the DB (newest first when
// fetched by id DESC) authored by op against project p, with action templated
// from actionTemplate. ts is offset by row index.
func seedAuditEntries(t *testing.T, db *gorm.DB, op *model.Operator, p *model.Project, action string, n int) {
	t.Helper()
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		ts := now.Add(time.Duration(-i) * time.Second)
		opID := op.ID
		row := model.AuditLog{
			Ts:         ts,
			OperatorID: &opID,
			Action:     action,
			Target:     "projects/p1/sites/s1",
			SourceIP:   "127.0.0.1",
			ExtraJSON:  "",
		}
		if p != nil {
			pid := p.ID
			row.ProjectID = &pid
		}
		require.NoError(t, db.Create(&row).Error)
	}
}

func newAuditService(t *testing.T, db *gorm.DB) *grpcsvc.AuditService {
	t.Helper()
	return grpcsvc.NewAuditService(dao.NewAuditDAO(db))
}

func TestListAuditLogsRequiresAuth(t *testing.T) {
	db := openTestDB(t)
	svc := newAuditService(t, db)

	_, err := svc.ListAuditLogs(context.Background(), &quicktunv1.ListAuditLogsRequest{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestListAuditLogsRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	user := seedOperator(t, db, "user@x.com", "p", false)
	svc := newAuditService(t, db)
	ctx := auth.WithOperator(context.Background(), user)

	_, err := svc.ListAuditLogs(ctx, &quicktunv1.ListAuditLogsRequest{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestListAuditLogsBasicFlow(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)

	p := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)

	seedAuditEntries(t, db, admin, p, "site.create", 3)

	svc := newAuditService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.ListAuditLogs(ctx, &quicktunv1.ListAuditLogsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetEntries(), 3)
	require.EqualValues(t, 3, resp.GetTotalSize())
	require.Empty(t, resp.GetNextPageToken())

	// Newest first means id DESC (id is monotonic with insertion order).
	require.True(t, resp.GetEntries()[0].GetId() > resp.GetEntries()[1].GetId())
	require.True(t, resp.GetEntries()[1].GetId() > resp.GetEntries()[2].GetId())

	// Joined fields populated.
	got := resp.GetEntries()[0]
	require.Equal(t, "admin@x.com", got.GetOperatorEmail())
	require.Equal(t, "p1", got.GetProjectSlug())
	require.Equal(t, "site.create", got.GetAction())
	require.NotNil(t, got.GetTime())
}

func TestListAuditLogsFiltersByOperator(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	other := seedOperator(t, db, "other@x.com", "p", true)

	p := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)

	seedAuditEntries(t, db, admin, p, "site.create", 2)
	seedAuditEntries(t, db, other, p, "site.update", 3)

	svc := newAuditService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.ListAuditLogs(ctx, &quicktunv1.ListAuditLogsRequest{OperatorEmail: "other@x.com"})
	require.NoError(t, err)
	require.Len(t, resp.GetEntries(), 3)
	require.EqualValues(t, 3, resp.GetTotalSize())
	for _, e := range resp.GetEntries() {
		require.Equal(t, "other@x.com", e.GetOperatorEmail())
	}
}

func TestListAuditLogsFiltersByActionPrefix(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)

	p := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)

	seedAuditEntries(t, db, admin, p, "site.create", 2)
	seedAuditEntries(t, db, admin, p, "site.delete", 1)
	seedAuditEntries(t, db, admin, p, "project.create", 2)

	svc := newAuditService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.ListAuditLogs(ctx, &quicktunv1.ListAuditLogsRequest{ActionPrefix: "site."})
	require.NoError(t, err)
	require.Len(t, resp.GetEntries(), 3)
	require.EqualValues(t, 3, resp.GetTotalSize())
	for _, e := range resp.GetEntries() {
		require.Contains(t, e.GetAction(), "site.")
	}
}

func TestListAuditLogsFiltersByProjectSlug(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)

	p1 := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	p2 := &model.Project{Slug: "p2", Name: "P2", RelayPortRange: "20100-20199", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p1).Error)
	require.NoError(t, db.Create(p2).Error)

	seedAuditEntries(t, db, admin, p1, "site.create", 2)
	seedAuditEntries(t, db, admin, p2, "site.create", 3)

	svc := newAuditService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.ListAuditLogs(ctx, &quicktunv1.ListAuditLogsRequest{ProjectSlug: "p2"})
	require.NoError(t, err)
	require.Len(t, resp.GetEntries(), 3)
	for _, e := range resp.GetEntries() {
		require.Equal(t, "p2", e.GetProjectSlug())
	}
}

func TestListAuditLogsPagination(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)

	p := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)

	// 7 rows, page size 3 → 3 + 3 + 1.
	seedAuditEntries(t, db, admin, p, "site.create", 7)

	svc := newAuditService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	page1, err := svc.ListAuditLogs(ctx, &quicktunv1.ListAuditLogsRequest{PageSize: 3})
	require.NoError(t, err)
	require.Len(t, page1.GetEntries(), 3)
	require.EqualValues(t, 7, page1.GetTotalSize())
	require.NotEmpty(t, page1.GetNextPageToken())

	page2, err := svc.ListAuditLogs(ctx, &quicktunv1.ListAuditLogsRequest{PageSize: 3, PageToken: page1.GetNextPageToken()})
	require.NoError(t, err)
	require.Len(t, page2.GetEntries(), 3)
	require.NotEmpty(t, page2.GetNextPageToken())

	page3, err := svc.ListAuditLogs(ctx, &quicktunv1.ListAuditLogsRequest{PageSize: 3, PageToken: page2.GetNextPageToken()})
	require.NoError(t, err)
	require.Len(t, page3.GetEntries(), 1)
	require.Empty(t, page3.GetNextPageToken())

	// No id overlap between consecutive pages.
	seenIDs := map[uint64]bool{}
	for _, p := range []*quicktunv1.ListAuditLogsResponse{page1, page2, page3} {
		for _, e := range p.GetEntries() {
			require.False(t, seenIDs[e.GetId()], "id %d duplicated across pages", e.GetId())
			seenIDs[e.GetId()] = true
		}
	}
	require.Len(t, seenIDs, 7)
}

func TestListAuditLogsRejectsInvalidPageToken(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)
	svc := newAuditService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	_, err := svc.ListAuditLogs(ctx, &quicktunv1.ListAuditLogsRequest{PageToken: "not-a-number"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestListAuditLogsCapsPageSize(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)

	p := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)

	// Seed > 200 to ensure cap applies.
	seedAuditEntries(t, db, admin, p, "site.create", 205)

	svc := newAuditService(t, db)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.ListAuditLogs(ctx, &quicktunv1.ListAuditLogsRequest{PageSize: 1000})
	require.NoError(t, err)
	require.Len(t, resp.GetEntries(), 200, "page_size should be capped at 200")
	require.NotEmpty(t, resp.GetNextPageToken())
}
