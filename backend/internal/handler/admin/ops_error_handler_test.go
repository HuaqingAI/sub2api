package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type opsErrorLogCaptureRepo struct {
	service.OpsRepository
	filter          *service.OpsErrorLogFilter
	breakdownFilter *service.OpsErrorLogFilter
}

func (r *opsErrorLogCaptureRepo) ListErrorLogs(_ context.Context, filter *service.OpsErrorLogFilter) (*service.OpsErrorLogList, error) {
	r.filter = filter
	return &service.OpsErrorLogList{Errors: []*service.OpsErrorLog{}, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *opsErrorLogCaptureRepo) GetAccountErrorBreakdown(_ context.Context, filter *service.OpsErrorLogFilter) (*service.OpsAccountErrorBreakdownResponse, error) {
	r.breakdownFilter = filter
	return &service.OpsAccountErrorBreakdownResponse{Accounts: []*service.OpsAccountErrorBreakdownItem{}}, nil
}

func newOpsErrorLogTestRouter(handler *OpsHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/errors", handler.GetErrorLogs)
	r.GET("/account-error-distribution", handler.GetAccountErrorBreakdown)
	r.GET("/request-errors", handler.ListRequestErrors)
	return r
}

func TestOpsErrorHandler_ForwardsRequestIDFilter(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "legacy errors", path: "/errors?request_id=req-admin-123"},
		{name: "request errors", path: "/request-errors?request_id=req-admin-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &opsErrorLogCaptureRepo{}
			svc := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			r := newOpsErrorLogTestRouter(NewOpsHandler(svc))

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200, body=%s", w.Code, w.Body.String())
			}
			if repo.filter == nil {
				t.Fatal("ListErrorLogs was not called")
			}
			if repo.filter.RequestID != "req-admin-123" {
				t.Fatalf("request_id filter=%q, want req-admin-123", repo.filter.RequestID)
			}
		})
	}
}

func TestOpsErrorHandler_ForwardsAccountErrorBreakdownFilters(t *testing.T) {
	repo := &opsErrorLogCaptureRepo{}
	svc := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	r := newOpsErrorLogTestRouter(NewOpsHandler(svc))

	start := time.Date(2026, time.October, 20, 1, 2, 3, 0, time.UTC)
	end := time.Date(2026, time.October, 20, 5, 6, 7, 0, time.UTC)
	query := url.Values{
		"start_time":   {start.Format(time.RFC3339)},
		"end_time":     {end.Format(time.RFC3339)},
		"view":         {"all"},
		"account_id":   {"42"},
		"user_id":      {"13"},
		"api_key_id":   {"7"},
		"group_id":     {"9"},
		"model":        {"gpt-4.1"},
		"category":     {"upstream"},
		"status_codes": {"429, 500"},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/account-error-distribution?"+query.Encode(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200, body=%s", w.Code, w.Body.String())
	}
	filter := repo.breakdownFilter
	if filter == nil {
		t.Fatal("GetAccountErrorBreakdown was not called")
	}
	if filter.StartTime == nil || !filter.StartTime.Equal(start) {
		t.Fatalf("start_time=%v, want %v", filter.StartTime, start)
	}
	if filter.EndTime == nil || !filter.EndTime.Equal(end) {
		t.Fatalf("end_time=%v, want %v", filter.EndTime, end)
	}
	if filter.View != opsListViewAll {
		t.Fatalf("view=%q, want %q", filter.View, opsListViewAll)
	}
	if filter.AccountID == nil || *filter.AccountID != 42 {
		t.Fatalf("account_id=%v, want 42", filter.AccountID)
	}
	if filter.UserID == nil || *filter.UserID != 13 {
		t.Fatalf("user_id=%v, want 13", filter.UserID)
	}
	if filter.APIKeyID == nil || *filter.APIKeyID != 7 {
		t.Fatalf("api_key_id=%v, want 7", filter.APIKeyID)
	}
	if filter.GroupID == nil || *filter.GroupID != 9 {
		t.Fatalf("group_id=%v, want 9", filter.GroupID)
	}
	if filter.Model != "gpt-4.1" {
		t.Fatalf("model=%q, want gpt-4.1", filter.Model)
	}
	if !reflect.DeepEqual(filter.ErrorPhasesAny, []string{"account_auth", "upstream", "network"}) {
		t.Fatalf("category phases=%v, want upstream category phases", filter.ErrorPhasesAny)
	}
	if !reflect.DeepEqual(filter.StatusCodes, []int{429, 500}) {
		t.Fatalf("status_codes=%v, want [429 500]", filter.StatusCodes)
	}
}
