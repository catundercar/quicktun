package model_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/model"
)

func TestServiceCreate(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)
	s := model.Site{ProjectID: p.ID, Name: "bastion"}
	require.NoError(t, db.Create(&s).Error)

	relayPort := uint16(20022)
	svc := model.Service{
		SiteID:     s.ID,
		Name:       "ssh",
		TargetAddr: "127.0.0.1",
		TargetPort: 22,
		Proto:      model.ProtoTCP,
		RelayPort:  &relayPort,
	}
	require.NoError(t, db.Create(&svc).Error)
	require.NotZero(t, svc.ID)
}

func TestServiceNameUniquePerSite(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)
	s := model.Site{ProjectID: p.ID, Name: "b"}
	require.NoError(t, db.Create(&s).Error)

	require.NoError(t, db.Create(&model.Service{SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP}).Error)
	err := db.Create(&model.Service{SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 23, Proto: model.ProtoTCP}).Error
	require.Error(t, err)
}

func TestServiceNameReusableAfterSoftDelete(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)
	s := model.Site{ProjectID: p.ID, Name: "b"}
	require.NoError(t, db.Create(&s).Error)

	svc1 := model.Service{SiteID: s.ID, Name: "reuse", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP}
	require.NoError(t, db.Create(&svc1).Error)
	require.NoError(t, db.Delete(&svc1).Error)

	svc2 := model.Service{SiteID: s.ID, Name: "reuse", TargetAddr: "127.0.0.1", TargetPort: 23, Proto: model.ProtoTCP}
	require.NoError(t, db.Create(&svc2).Error)
	require.NotEqual(t, svc1.ID, svc2.ID)
}

func TestServiceRelayPortNullableUntilAllocated(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)
	s := model.Site{ProjectID: p.ID, Name: "b"}
	require.NoError(t, db.Create(&s).Error)

	// Service created without RelayPort → stored as NULL
	svc := model.Service{SiteID: s.ID, Name: "unassigned", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP}
	require.NoError(t, db.Create(&svc).Error)
	require.Nil(t, svc.RelayPort)

	// Reload from DB and confirm round-trip preserves NULL
	var got model.Service
	require.NoError(t, db.First(&got, svc.ID).Error)
	require.Nil(t, got.RelayPort)

	// SQL-level: WHERE relay_port IS NULL finds unassigned services
	var nullCount int64
	require.NoError(t, db.Model(&model.Service{}).Where("relay_port IS NULL").Count(&nullCount).Error)
	require.Equal(t, int64(1), nullCount)

	// Assign a port and verify the IS NULL query no longer matches
	port := uint16(20022)
	got.RelayPort = &port
	require.NoError(t, db.Save(&got).Error)
	require.NoError(t, db.Model(&model.Service{}).Where("relay_port IS NULL").Count(&nullCount).Error)
	require.Equal(t, int64(0), nullCount)
}
