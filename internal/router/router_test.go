package router

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Xairlepl/go-shorturl/internal/handler"
	"github.com/Xairlepl/go-shorturl/pkg/jwtutil"
)

// newTestEngine 只验证路由装配与中间件，service 依赖传 nil：
// 被测路径不会走到 service 层。
func newTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	return New(Deps{
		Auth:   handler.NewAuthHandler(nil),
		URL:    handler.NewURLHandler(nil),
		JWT:    jwtutil.NewManager("test-secret", time.Hour),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// 根路径的 /:code 与静态路由 /healthz、/api 是兄弟节点，
// 这里确认 gin 能同时注册两者而不 panic。
func TestRouteRegistrationDoesNotConflict(t *testing.T) {
	engine := newTestEngine(t)

	want := []string{
		"GET /healthz",
		"GET /:code",
		"POST /api/v1/auth/register",
		"POST /api/v1/auth/login",
		"POST /api/v1/urls",
		"GET /api/v1/urls",
		"DELETE /api/v1/urls/:id",
		"GET /api/v1/urls/:id/stats",
	}

	registered := make(map[string]bool)
	for _, r := range engine.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	for _, key := range want {
		if !registered[key] {
			t.Errorf("route %q was not registered", key)
		}
	}
}

func TestHealthz(t *testing.T) {
	engine := newTestEngine(t)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// /healthz 是静态路由，不能被 /:code 通配符抢走。
func TestHealthzTakesPrecedenceOverWildcard(t *testing.T) {
	engine := newTestEngine(t)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if body := w.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("body = %s, want healthz response", body)
	}
}

func TestProtectedRoutesRequireAuth(t *testing.T) {
	engine := newTestEngine(t)

	cases := []struct {
		method string
		path   string
		header string
	}{
		{http.MethodGet, "/api/v1/urls", ""},
		{http.MethodPost, "/api/v1/urls", ""},
		{http.MethodDelete, "/api/v1/urls/1", ""},
		{http.MethodGet, "/api/v1/urls/1/stats", ""},
		{http.MethodGet, "/api/v1/urls", "Bearer garbage"},
		{http.MethodGet, "/api/v1/urls", "garbage"},
		{http.MethodGet, "/api/v1/urls", "Basic dXNlcjpwYXNz"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s (auth=%q) status = %d, want 401", tc.method, tc.path, tc.header, w.Code)
		}
	}
}

func TestRequestIDHeaderIsSet(t *testing.T) {
	engine := newTestEngine(t)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if w.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header is empty")
	}
}

func TestRequestIDIsPropagated(t *testing.T) {
	engine := newTestEngine(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "trace-abc")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-ID"); got != "trace-abc" {
		t.Errorf("X-Request-ID = %q, want %q", got, "trace-abc")
	}
}
