package api_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"log/slog"
	"os"

	"github.com/waitmesh/waitmesh/internal/api"
	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/config"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

func TestAuth_BearerRequiredAndHealthPublic(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.New(db, clock.System{}, log, 64)
	defer eng.Close()
	cfg := config.Default()
	cfg.AuthToken = "s3cret"
	srv := api.New(eng, cfg, log)

	// Events without token → 401
	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("events: %d", rec.Code)
	}

	// Wrong token → 401
	req = httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rec.Code)
	}

	// Health live without token → 200
	req = httptest.NewRequest(http.MethodGet, "/v1/health/live", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("live: %d", rec.Code)
	}

	// Ready without token → 200
	req = httptest.NewRequest(http.MethodGet, "/v1/health/ready", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ready: %d", rec.Code)
	}

	// Events with correct token → 200
	req = httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authed events: %d body=%s", rec.Code, rec.Body.String())
	}
}
