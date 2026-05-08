package dao_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

// seedAuditFixtures inserts:
//   - 2 operators (alice, bob)
//   - 2 projects (p1, p2)
//   - audit rows tying them together with varying ts / actions.
//
// Returns the (alice, bob) operator IDs and (p1, p2) project IDs so callers
// can build filter inputs.
func seedAuditFixtures(t *testing.T, db *gorm.DB, base time.Time) (aliceID, bobID, p1ID, p2ID uint64) {
	t.Helper()
	alice := model.Operator{Email: "alice@x.com", PasswordHash: "h"}
	bob := model.Operator{Email: "bob@x.com", PasswordHash: "h"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)

	p1 := model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099"}
	p2 := model.Project{Slug: "p2", Name: "P2", RelayPortRange: "20100-20199"}
	require.NoError(t, db.Create(&p1).Error)
	require.NoError(t, db.Create(&p2).Error)

	// Helper to insert an audit row with explicit ts (so order is deterministic).
	insert := func(ts time.Time, opID, projID uint64, action, target string) {
		t.Helper()
		op := opID
		pj := projID
		row := model.AuditLog{
			Ts:         ts,
			OperatorID: &op,
			ProjectID:  &pj,
			Action:     action,
			Target:     target,
			SourceIP:   "127.0.0.1",
			ExtraJSON:  "",
		}
		require.NoError(t, db.Create(&row).Error)
	}

	// 5 rows total: 3 by alice / 2 by bob, spanning p1 and p2.
	insert(base.Add(-50*time.Second), alice.ID, p1.ID, "site.create", "projects/p1/sites/s1")
	insert(base.Add(-40*time.Second), alice.ID, p1.ID, "site.update", "projects/p1/sites/s1")
	insert(base.Add(-30*time.Second), bob.ID, p2.ID, "project.create", "projects/p2")
	insert(base.Add(-20*time.Second), alice.ID, p2.ID, "service.create", "projects/p2/sites/s1/services/svc-a")
	insert(base.Add(-10*time.Second), bob.ID, p1.ID, "site.delete", "projects/p1/sites/s2")

	return alice.ID, bob.ID, p1.ID, p2.ID
}

func TestAuditListNewestFirst(t *testing.T) {
	db := openWithModels(t)
	d := dao.NewAuditDAO(db)
	base := time.Now().UTC()
	seedAuditFixtures(t, db, base)

	rows, err := d.List(context.Background(), dao.AuditListFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 5)

	// Newest first: site.delete (-10s) should lead.
	require.Equal(t, "site.delete", rows[0].Action)
	require.Equal(t, "site.create", rows[4].Action)
	// joined columns populated
	require.Equal(t, "bob@x.com", rows[0].OperatorEmail)
	require.Equal(t, "p1", rows[0].ProjectSlug)
}

func TestAuditListFilterByOperatorEmail(t *testing.T) {
	db := openWithModels(t)
	d := dao.NewAuditDAO(db)
	seedAuditFixtures(t, db, time.Now().UTC())

	rows, err := d.List(context.Background(), dao.AuditListFilter{
		OperatorEmail: "alice@x.com",
		Limit:         10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	for _, r := range rows {
		require.Equal(t, "alice@x.com", r.OperatorEmail)
	}
}

func TestAuditListFilterByProjectSlug(t *testing.T) {
	db := openWithModels(t)
	d := dao.NewAuditDAO(db)
	seedAuditFixtures(t, db, time.Now().UTC())

	rows, err := d.List(context.Background(), dao.AuditListFilter{
		ProjectSlug: "p2",
		Limit:       10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		require.Equal(t, "p2", r.ProjectSlug)
	}
}

func TestAuditListFilterByActionPrefix(t *testing.T) {
	db := openWithModels(t)
	d := dao.NewAuditDAO(db)
	seedAuditFixtures(t, db, time.Now().UTC())

	rows, err := d.List(context.Background(), dao.AuditListFilter{
		ActionPrefix: "site.",
		Limit:        10,
	})
	require.NoError(t, err)
	// 3 site.* rows: site.create, site.update, site.delete
	require.Len(t, rows, 3)
	for _, r := range rows {
		require.Contains(t, r.Action, "site.")
	}
}

func TestAuditListFilterBySinceUntil(t *testing.T) {
	db := openWithModels(t)
	d := dao.NewAuditDAO(db)
	base := time.Now().UTC()
	seedAuditFixtures(t, db, base)

	since := base.Add(-35 * time.Second)
	until := base.Add(-15 * time.Second)
	rows, err := d.List(context.Background(), dao.AuditListFilter{
		Since: &since,
		Until: &until,
		Limit: 10,
	})
	require.NoError(t, err)
	// rows at -30s and -20s only
	require.Len(t, rows, 2)
}

func TestAuditListPaginationCursor(t *testing.T) {
	db := openWithModels(t)
	d := dao.NewAuditDAO(db)
	base := time.Now().UTC()
	seedAuditFixtures(t, db, base)

	// First page: limit 2 (ask for 3 to detect "next").
	page1, err := d.List(context.Background(), dao.AuditListFilter{Limit: 3})
	require.NoError(t, err)
	require.Len(t, page1, 3)

	// Use last id of page1 as cursor for page2.
	cursor := page1[2].ID
	page2, err := d.List(context.Background(), dao.AuditListFilter{
		Limit:   10,
		AfterID: cursor,
	})
	require.NoError(t, err)
	require.Len(t, page2, 2)

	// No id overlap.
	for _, r := range page2 {
		for _, p := range page1 {
			require.NotEqual(t, p.ID, r.ID, "page2 must not repeat ids from page1")
		}
	}
}

func TestAuditCountMatchesFilter(t *testing.T) {
	db := openWithModels(t)
	d := dao.NewAuditDAO(db)
	seedAuditFixtures(t, db, time.Now().UTC())

	total, err := d.Count(context.Background(), dao.AuditListFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 5, total)

	filtered, err := d.Count(context.Background(), dao.AuditListFilter{
		OperatorEmail: "alice@x.com",
	})
	require.NoError(t, err)
	require.EqualValues(t, 3, filtered)
}
