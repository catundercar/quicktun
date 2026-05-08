package grpcsvc

import (
	"context"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
)

const (
	auditDefaultLimit = 50
	auditMaxLimit     = 200
)

// AuditService implements quicktunv1.AuditServiceServer. Admin-only; surfaces
// the append-only audit_logs table to the web admin and external monitoring.
type AuditService struct {
	quicktunv1.UnimplementedAuditServiceServer
	audit *dao.AuditDAO
}

// NewAuditService constructs an AuditService bound to d.
func NewAuditService(d *dao.AuditDAO) *AuditService {
	return &AuditService{audit: d}
}

// ListAuditLogs returns audit log entries newest-first with optional filters
// and cursor pagination. Admin-only.
//
// Cursor: page_token is the id of the last entry returned by the previous
// page (encoded as a base-10 string). The DAO's AfterID acts as a strict
// upper bound (id < cursor) on the descending-id ordering, so the client can
// safely loop until next_page_token is empty.
func (a *AuditService) ListAuditLogs(ctx context.Context, req *quicktunv1.ListAuditLogsRequest) (*quicktunv1.ListAuditLogsResponse, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin only")
	}

	limit := int(req.GetPageSize())
	if limit <= 0 {
		limit = auditDefaultLimit
	}
	if limit > auditMaxLimit {
		limit = auditMaxLimit
	}

	var afterID uint64
	if t := req.GetPageToken(); t != "" {
		n, err := strconv.ParseUint(t, 10, 64)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
		afterID = n
	}

	f := dao.AuditListFilter{
		OperatorEmail: req.GetOperatorEmail(),
		ProjectSlug:   req.GetProjectSlug(),
		ActionPrefix:  req.GetActionPrefix(),
		Limit:         limit + 1, // +1 to detect "has next page" without a separate count
		AfterID:       afterID,
	}
	if req.GetSince() != nil {
		t := req.GetSince().AsTime()
		f.Since = &t
	}
	if req.GetUntil() != nil {
		t := req.GetUntil().AsTime()
		f.Until = &t
	}

	rows, err := a.audit.List(ctx, f)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	// total_size is computed against the same filter without limit/cursor.
	// AfterID is intentionally cleared so total_size reflects the full
	// filtered set rather than only "remaining after cursor".
	totalFilter := f
	totalFilter.Limit = 0
	totalFilter.AfterID = 0
	total, err := a.audit.Count(ctx, totalFilter)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := &quicktunv1.ListAuditLogsResponse{TotalSize: total}
	if len(rows) > limit {
		// We over-fetched by one to detect the next page; drop the sentinel
		// and record the cursor.
		resp.NextPageToken = strconv.FormatUint(rows[limit-1].ID, 10)
		rows = rows[:limit]
	}

	for _, r := range rows {
		e := &quicktunv1.AuditLogEntry{
			Id:            r.ID,
			OperatorEmail: r.OperatorEmail,
			SourceIp:      r.SourceIP,
			Action:        r.Action,
			Target:        r.Target,
			ProjectSlug:   r.ProjectSlug,
			ExtraJson:     r.ExtraJSON,
		}
		if !r.Ts.IsZero() {
			e.Time = timestamppb.New(r.Ts)
		}
		resp.Entries = append(resp.Entries, e)
	}
	return resp, nil
}
