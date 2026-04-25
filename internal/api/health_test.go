package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealth_Returns200(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	healthHandler().ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	body, _ := io.ReadAll(w.Body)
	var got map[string]string
	assert.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "ok", got["status"])
}
