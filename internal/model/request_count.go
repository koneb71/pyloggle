package model

import (
	"errors"
	"math"
	"strings"
	"time"
)

// RequestCount tallies the requests to one endpoint during one minute: how many there were, how
// many failed (status 500 or above), how long each took and who made them (user id hashes).
//
// The Python package sends these instead of a line per request (POST /v1/ingest/requests, one
// JSON object per line), and ingest turns every request-shaped event into one (CountOf). So each
// request is counted once, in one place, whether or not it is also listed on Logs.
type RequestCount struct {
	Minute      Timestamp `json:"minute"`
	ProjectID   string    `json:"project_id"` // always overwritten by the resolved tenant
	Service     string    `json:"service"`
	Env         string    `json:"env"`
	Release     string    `json:"release,omitempty"`
	Method      string    `json:"method"`
	Route       string    `json:"route"`
	Requests    uint32    `json:"requests"`
	Errors      uint32    `json:"errors"`
	DurationsMS []float32 `json:"durations_ms,omitempty"`
	Users       []string  `json:"users,omitempty"`
}

// MaxCountedRequests bounds one count. The Python package sends a count when its minute ends or
// when it reaches 1,000 requests, so a sane sender stays far below it.
const MaxCountedRequests = 100_000

const (
	maxRouteBytes  = 1024
	maxUserIDBytes = 128
)

// Normalize checks a count from the wire and puts it in canonical form: an upper-case method,
// the minute it belongs to, response times that are numbers, and distinct people. now fills a
// missing minute.
func (c *RequestCount) Normalize(now time.Time) error {
	c.Method = strings.ToUpper(strings.TrimSpace(c.Method))
	c.Route = CutUTF8(strings.TrimSpace(c.Route), maxRouteBytes)
	switch {
	case c.Method == "" || c.Route == "":
		return errors.New("a request count needs a method and a route")
	case c.Requests == 0:
		return errors.New("a request count needs at least one request")
	case c.Requests > MaxCountedRequests:
		return errors.New("a request count holds at most 100000 requests")
	case c.Errors > c.Requests:
		return errors.New("a request count cannot have more failures than requests")
	case len(c.DurationsMS) > int(c.Requests):
		return errors.New("a request count cannot have more response times than requests")
	}
	if c.Minute.IsZero() {
		c.Minute = At(now)
	}
	c.Minute = At(c.Minute.UTC().Truncate(time.Minute))

	kept := c.DurationsMS[:0]
	for _, d := range c.DurationsMS {
		if d > 0 && !math.IsInf(float64(d), 0) && !math.IsNaN(float64(d)) {
			kept = append(kept, d)
		}
	}
	c.DurationsMS = kept

	seen := make(map[string]bool, len(c.Users))
	users := c.Users[:0]
	for _, u := range c.Users {
		u = strings.TrimSpace(u)
		if u == "" || len(u) > maxUserIDBytes || seen[u] || len(users) == int(c.Requests) {
			continue
		}
		seen[u] = true
		users = append(users, u)
	}
	c.Users = users
	return nil
}

// CountOf returns the count for one request-shaped event: a request line (a method, a route and
// a response time) or a request that crashed (an exception answered with status 500 or above).
// Anything else is not a request and is not counted.
func CountOf(ev Event) (RequestCount, bool) {
	h := ev.HTTP
	if h == nil || h.Method == "" || h.Route == "" {
		return RequestCount{}, false
	}
	crashed := ev.Exception != nil && h.Status >= 500
	if ev.DurationMS <= 0 && !crashed {
		return RequestCount{}, false
	}
	c := RequestCount{
		Minute:    At(ev.Timestamp.UTC().Truncate(time.Minute)),
		ProjectID: ev.ProjectID,
		Service:   ev.Service,
		Env:       ev.Env,
		Release:   ev.Release,
		Method:    strings.ToUpper(h.Method),
		Route:     CutUTF8(h.Route, maxRouteBytes),
		Requests:  1,
	}
	if h.Status >= 500 {
		c.Errors = 1
	}
	if ev.DurationMS > 0 {
		c.DurationsMS = []float32{float32(ev.DurationMS)}
	}
	if ev.User != nil && ev.User.IDHash != "" && len(ev.User.IDHash) <= maxUserIDBytes {
		c.Users = []string{ev.User.IDHash}
	}
	return c, true
}
