package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/bcrisp4/wire/internal/discover"
)

// discoverRequest is the wire format of POST /api/v1/feeds/discover.
type discoverRequest struct {
	URL string `json:"url"`
}

// discoverCandidate is the wire format of one item in the response array.
// We use a JSON-tagged struct rather than reusing discover.Candidate so the
// HTTP shape is decoupled from the internal package.
type discoverCandidate struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

type discoverResponse struct {
	Candidates []discoverCandidate `json:"candidates"`
}

// discoverHandler returns POST /api/v1/feeds/discover. The handler delegates
// HTTP fetching to the supplied client so tests can inject httptest servers.
func discoverHandler(client *http.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req discoverRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.URL) == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}

		cands, err := discover.Discover(r.Context(), client, req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		out := discoverResponse{Candidates: make([]discoverCandidate, 0, len(cands))}
		for _, c := range cands {
			out.Candidates = append(out.Candidates, discoverCandidate{
				URL:   c.URL,
				Title: c.Title,
				Type:  c.Type,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
}
