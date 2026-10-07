package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/db"
)

func TestHealthz(t *testing.T) {
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	defer d.Close()
	h := NewServer(d, "test-secret", time.UTC, nil).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	d.Close()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "closed db")
}

func TestStaticFiles(t *testing.T) {
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	defer d.Close()
	h := NewServer(d, "test-secret", time.UTC, nil).Handler()

	for _, path := range []string{"/static/style.css", "/static/htmx.min.js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		require.Equal(t, http.StatusOK, rec.Code, path)
		require.NotEmpty(t, rec.Body.String(), path)
	}
}
