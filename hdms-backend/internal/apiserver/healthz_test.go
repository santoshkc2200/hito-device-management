package apiserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hito-hospital/hdms/internal/apiserver"
)

// Clients decide whether to show the non-production banner from /healthz, so
// it must report the environment the process was configured with.
func TestHealthzReportsEnvironment(t *testing.T) {
	t.Parallel()

	for _, env := range []string{"development", "staging", "production"} {
		srv := apiserver.New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, env, apiserver.BackupConsoleConfig{})
		rec := httptest.NewRecorder()
		srv.GetHealthz(rec, httptest.NewRequest(http.MethodGet, "/v1/healthz", nil))

		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			Status      string `json:"status"`
			Environment string `json:"environment"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, "ok", body.Status)
		require.Equal(t, env, body.Environment)
	}
}
