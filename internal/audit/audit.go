// Package audit writes structured entries to the audit_logs table.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/model"
)

// Entry describes one audit event. Action and Target are required; everything
// else is filled from context or left null.
type Entry struct {
	ProjectID *uint64
	Action    string
	Target    string
	Extra     map[string]any
}

type sourceIPCtxKey struct{}

// WithSourceIP attaches a source IP to ctx so writes pick it up automatically.
// Set this in your gRPC interceptor (typically from peer.FromContext).
func WithSourceIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, sourceIPCtxKey{}, ip)
}

func sourceIPFromContext(ctx context.Context) string {
	v, _ := ctx.Value(sourceIPCtxKey{}).(string)
	return v
}

// Writer persists audit events.
type Writer struct{ db *gorm.DB }

// NewWriter constructs a Writer bound to db.
func NewWriter(db *gorm.DB) *Writer { return &Writer{db: db} }

// Log inserts one audit entry. The acting operator and source IP are pulled
// from ctx; Action and Target come from e.
func (w *Writer) Log(ctx context.Context, e Entry) error {
	if e.Action == "" {
		return fmt.Errorf("audit: action is required")
	}

	var operatorID *uint64
	if op := auth.OperatorFromContext(ctx); op != nil {
		opID := op.ID
		operatorID = &opID
	}

	extra := ""
	if len(e.Extra) > 0 {
		b, err := json.Marshal(e.Extra)
		if err != nil {
			return fmt.Errorf("audit: marshal extra: %w", err)
		}
		extra = string(b)
	}

	row := model.AuditLog{
		Ts:         time.Now().UTC(),
		ProjectID:  e.ProjectID,
		OperatorID: operatorID,
		Action:     e.Action,
		Target:     e.Target,
		SourceIP:   sourceIPFromContext(ctx),
		ExtraJSON:  extra,
	}
	if err := w.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("audit: create: %w", err)
	}
	return nil
}
