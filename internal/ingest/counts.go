package ingest

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/koneb71/pyloggle/internal/metrics"
	"github.com/koneb71/pyloggle/internal/model"
)

// maxCountLines bounds one request-counts batch; the Python package sends one line per endpoint
// and minute, so a busy app sends a few dozen.
const maxCountLines = 10_000

// Counts is the request-counts door (POST /v1/ingest/requests): one JSON object per line, each a
// model.RequestCount. Same keys and scopes as /v1/ingest. Older Python packages never call it;
// newer ones stop counting when an older server answers 404.
func (h *Handler) Counts() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, err := h.p.Authenticate(r.Context(), r)
		if err != nil {
			switch {
			case errors.Is(err, ErrUnauthorized):
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			case errors.Is(err, ErrInsufficientScope):
				http.Error(w, err.Error(), http.StatusForbidden)
			default:
				http.Error(w, "auth lookup failed", http.StatusInternalServerError)
			}
			return
		}
		body, closer, err := maybeGzipBody(r)
		if err != nil {
			http.Error(w, "bad gzip body", http.StatusBadRequest)
			return
		}
		if closer != nil {
			defer closer.Close()
		}

		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
		var (
			counts   []model.RequestCount
			errs     []string
			rejected int
			rawBytes int
			lineNo   int
		)
		reject := func(err error) {
			rejected++
			metrics.IngestRejected.Inc("requests")
			if len(errs) < 5 {
				errs = append(errs, "line "+strconv.Itoa(lineNo)+": "+err.Error())
			}
		}
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			lineNo++
			if lineNo > maxCountLines {
				http.Error(w, "too many lines: at most "+strconv.Itoa(maxCountLines)+" per request", http.StatusRequestEntityTooLarge)
				return
			}
			rawBytes += len(line)
			var c model.RequestCount
			if err := json.Unmarshal([]byte(line), &c); err != nil {
				reject(err)
				continue
			}
			counts = append(counts, c)
		}
		if err := sc.Err(); err != nil && err != io.EOF {
			metrics.IngestRequests.Inc("requests", "400")
			http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		accepted, invalid, outcome := h.p.ProcessRequestCounts(r.Context(), t, counts, rawBytes)
		for _, err := range invalid {
			reject(err)
		}
		metrics.IngestBytes.Add(float64(rawBytes), t.ProjectID, "requests")
		metrics.IngestRequests.Inc("requests", "202")

		resp := map[string]any{"accepted": accepted, "rejected": rejected}
		if outcome != Accepted {
			resp["dropped"] = map[string]int{outcome.String(): len(counts) - len(invalid)}
		}
		if len(errs) > 0 {
			resp["errors"] = errs
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(resp)
	})
}
