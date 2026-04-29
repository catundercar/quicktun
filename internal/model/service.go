package model

// Proto is the wire protocol of a Service. Phase 1 ships TCP only.
type Proto string

const (
	ProtoTCP Proto = "tcp"
	ProtoUDP Proto = "udp" // Phase 2
)

// Service is a TCP target reachable via the bastion. target_addr can be
// 127.0.0.1 (the bastion itself) or any LAN IP the bastion can reach.
//
// RelayPort is allocated by the control plane within the project's port range.
type Service struct {
	Base
	SiteID     uint64 `gorm:"index;uniqueIndex:uk_site_svc_name_active,where:deleted_at IS NULL;not null" json:"site_id"`
	Name       string `gorm:"uniqueIndex:uk_site_svc_name_active,where:deleted_at IS NULL;not null;size:64" json:"name"`
	TargetAddr string `gorm:"not null;size:64" json:"target_addr"`
	TargetPort uint16 `gorm:"not null" json:"target_port"`
	Proto      Proto  `gorm:"not null;default:'tcp';size:8" json:"proto"`
	RelayPort  *uint16 `gorm:"index" json:"relay_port,omitempty"`
}
