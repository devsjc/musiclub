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
	h := NewServer(d, "test-secret", time.UTC).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	d.Close()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "closed db")
}
