package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type opsErrorLogCaptureRepo struct {
	service.OpsRepository
	filter *service.OpsErrorLogFilter
}

func (r *opsErrorLogCaptureRepo) ListErrorLogs(_ context.Context, filter *service.OpsErrorLogFilter) (*service.OpsErrorLogList, error) {
	r.filter = filter
	return &service.OpsErrorLogList{Errors: []*service.OpsErrorLog{}, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func newOpsErrorLogTestRouter(handler *OpsHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/errors", handler.GetErrorLogs)
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
