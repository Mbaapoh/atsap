// Package rpc is ReportingService's ConnectRPC surface (LLD-09).
package rpc

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	"atsap-api/internal/reporting/application"
	"atsap-api/internal/reporting/domain"
	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/shared/wire"
)

// Handler implements ReportingService.
type Handler struct {
	svc *application.Service
	atsapbxv1connect.UnimplementedReportingServiceHandler
}

var _ atsapbxv1connect.ReportingServiceHandler = (*Handler)(nil)

// NewHandler returns the reporting handler.
func NewHandler(svc *application.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListCallRecords(ctx context.Context, req *connect.Request[atsapbxv1.ListCallRecordsRequest]) (*connect.Response[atsapbxv1.ListCallRecordsResponse], error) {
	tenantID, filter, err := parse(req.Msg.GetTenantId(), req.Msg.GetFilter())
	if err != nil {
		return nil, err
	}
	limit := int(req.Msg.GetLimit())
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	records, err := h.svc.ListCallRecords(ctx, filter, application.Page{Limit: limit, Offset: max(int(req.Msg.GetOffset()), 0)})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("reporting read failed"))
	}
	_ = tenantID
	out := make([]*atsapbxv1.CallRecord, 0, len(records))
	for _, r := range records {
		out = append(out, toProtoRecord(r))
	}
	return connect.NewResponse(&atsapbxv1.ListCallRecordsResponse{Records: out}), nil
}

func (h *Handler) ExportCallRecords(ctx context.Context, req *connect.Request[atsapbxv1.ExportCallRecordsRequest]) (*connect.Response[atsapbxv1.ExportCallRecordsResponse], error) {
	_, filter, err := parse(req.Msg.GetTenantId(), req.Msg.GetFilter())
	if err != nil {
		return nil, err
	}
	csv, err := h.svc.ExportCallRecords(ctx, filter)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("reporting export failed"))
	}
	return connect.NewResponse(&atsapbxv1.ExportCallRecordsResponse{Csv: csv}), nil
}

func parse(tenant string, f *atsapbxv1.CallRecordFilter) (shareddomain.TenantID, domain.Filter, error) {
	tenantID, err := shareddomain.ParseTenantID(tenant)
	if err != nil {
		return shareddomain.TenantID{}, domain.Filter{}, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	out := domain.Filter{}
	if f != nil {
		if f.GetFrom() != nil {
			out.From = f.GetFrom().AsTime()
		}
		if f.GetTo() != nil {
			out.To = f.GetTo().AsTime()
		}
		out.Direction = f.GetDirection()
		out.Number = f.GetNumber()
		if f.GetExtensionId() != "" {
			id, err := shareddomain.ParseExtensionID(f.GetExtensionId())
			if err != nil {
				return tenantID, out, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid extension_id"))
			}
			out.ExtensionID = &id
		}
		if f.GetPrincipalId() != "" {
			id, err := shareddomain.ParsePrincipalID(f.GetPrincipalId())
			if err != nil {
				return tenantID, out, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid principal_id"))
			}
			out.PrincipalID = &id
		}
	}
	return tenantID, out, nil
}

func toProtoRecord(r domain.CallRecord) *atsapbxv1.CallRecord {
	out := &atsapbxv1.CallRecord{
		CallId: r.CallID.String(), Direction: r.Direction, SourceNumber: r.SourceNumber, DestNumber: r.DestNumber,
		StartedAt: timestamppb.New(r.StartedAt), TerminationReason: r.TerminationReason,
	}
	if r.AnsweredAt != nil {
		out.AnsweredAt = timestamppb.New(*r.AnsweredAt)
	}
	if r.EndedAt != nil {
		out.EndedAt = timestamppb.New(*r.EndedAt)
	}
	if r.AnsweredAt != nil && r.EndedAt != nil {
		out.DurationSeconds = wire.Int32(int64(r.EndedAt.Sub(*r.AnsweredAt) / time.Second))
	}
	for _, p := range r.Participants {
		part := &atsapbxv1.CallRecordParticipant{
			ParticipantId: p.ID.String(), Role: p.Role,
			BillableSeconds: wire.Int32(int64(p.BillableSeconds)), UsageSeconds: wire.Int32(int64(p.UsageSeconds)),
		}
		if p.ExtensionID != nil {
			part.ExtensionId = p.ExtensionID.String()
		}
		if p.PrincipalID != nil {
			part.PrincipalId = p.PrincipalID.String()
		}
		out.Participants = append(out.Participants, part)
	}
	return out
}

// TenantID tells the auth interceptor which body field must match the
// token.
func TenantID(request any) (string, bool) {
	switch r := request.(type) {
	case *atsapbxv1.ListCallRecordsRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	case *atsapbxv1.ExportCallRecordsRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	default:
		return "", false
	}
}
