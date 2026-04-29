package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/model"
)

func TestSiteCreate(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)

	s := model.Site{
		ProjectID:    p.ID,
		Name:         "hospital-shanghai",
		LanCidrsJSON: `["192.168.10.0/24"]`,
		Mode:         model.SiteModeEndpoint,
		Status:       model.SiteStatusPending,
	}
	require.NoError(t, db.Create(&s).Error)
	require.NotZero(t, s.ID)
}

func TestSiteNameUniquePerProject(t *testing.T) {
	db := openMemDB(t)

	p1 := model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099"}
	p2 := model.Project{Slug: "p2", Name: "P2", RelayPortRange: "20100-20199"}
	require.NoError(t, db.Create(&p1).Error)
	require.NoError(t, db.Create(&p2).Error)

	require.NoError(t, db.Create(&model.Site{ProjectID: p1.ID, Name: "bastion"}).Error)
	// same name in different project = OK
	require.NoError(t, db.Create(&model.Site{ProjectID: p2.ID, Name: "bastion"}).Error)
	// duplicate in same project = fails
	err := db.Create(&model.Site{ProjectID: p1.ID, Name: "bastion"}).Error
	require.Error(t, err)
}

func TestSiteAgentTokenOnePerSite(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)
	s := model.Site{ProjectID: p.ID, Name: "b1"}
	require.NoError(t, db.Create(&s).Error)

	exp := time.Now().UTC().Add(24 * time.Hour)
	tok := model.SiteAgentToken{SiteID: s.ID, TokenHash: "h1", ExpiresAt: &exp}
	require.NoError(t, db.Create(&tok).Error)

	dup := model.SiteAgentToken{SiteID: s.ID, TokenHash: "h2"}
	err := db.Create(&dup).Error
	require.Error(t, err)
}

func TestSiteNameReusableAfterSoftDelete(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)

	s1 := model.Site{ProjectID: p.ID, Name: "reuse"}
	require.NoError(t, db.Create(&s1).Error)
	require.NoError(t, db.Delete(&s1).Error)

	s2 := model.Site{ProjectID: p.ID, Name: "reuse"}
	require.NoError(t, db.Create(&s2).Error)
	require.NotEqual(t, s1.ID, s2.ID)
}

func TestSiteAgentTokenReusableAfterSoftDelete(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)
	s := model.Site{ProjectID: p.ID, Name: "b"}
	require.NoError(t, db.Create(&s).Error)

	t1 := model.SiteAgentToken{SiteID: s.ID, TokenHash: "h"}
	require.NoError(t, db.Create(&t1).Error)
	require.NoError(t, db.Delete(&t1).Error)

	// Same SiteID should be re-creatable; same TokenHash should be re-creatable.
	t2 := model.SiteAgentToken{SiteID: s.ID, TokenHash: "h"}
	require.NoError(t, db.Create(&t2).Error)
	require.NotEqual(t, t1.ID, t2.ID)
}
