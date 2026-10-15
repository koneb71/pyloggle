package model

import (
	"math"
	"testing"
	"time"
)

func TestRequestCountNormalize(t *testing.T) {
	now := time.Date(2026, 10, 15, 4, 12, 31, 0, time.UTC)
	c := RequestCount{
		Method: " post ", Route: "/orders", Requests: 4, Errors: 1,
		DurationsMS: []float32{12.5, 0, float32(math.NaN()), 8},
		Users:       []string{"a", "", "a", "b"},
	}
	if err := c.Normalize(now); err != nil {
		t.Fatal(err)
	}
	if c.Method != "POST" || !c.Minute.Equal(now.Truncate(time.Minute)) {
		t.Fatalf("method or minute: %+v", c)
	}
	if len(c.DurationsMS) != 2 || c.DurationsMS[0] != 12.5 || c.DurationsMS[1] != 8 {
		t.Fatalf("only real response times stay: %v", c.DurationsMS)
	}
	if len(c.Users) != 2 || c.Users[0] != "a" || c.Users[1] != "b" {
		t.Fatalf("people are distinct and non-empty: %v", c.Users)
	}
	at := RequestCount{Minute: At(time.Date(2026, 10, 15, 3, 59, 59, 0, time.UTC)), Method: "GET", Route: "/", Requests: 1}
	if err := at.Normalize(now); err != nil || !at.Minute.Equal(time.Date(2026, 10, 15, 3, 59, 0, 0, time.UTC)) {
		t.Fatalf("a sent minute is kept, truncated: %v %v", at.Minute, err)
	}

	for name, bad := range map[string]RequestCount{
		"no method":           {Route: "/", Requests: 1},
		"no route":            {Method: "GET", Requests: 1},
		"no requests":         {Method: "GET", Route: "/"},
		"too many":            {Method: "GET", Route: "/", Requests: MaxCountedRequests + 1},
		"more failures":       {Method: "GET", Route: "/", Requests: 1, Errors: 2},
		"more response times": {Method: "GET", Route: "/", Requests: 1, DurationsMS: []float32{1, 2}},
	} {
		if err := bad.Normalize(now); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
}

func TestCountOf(t *testing.T) {
	ts := At(time.Date(2026, 10, 15, 4, 12, 31, 0, time.UTC))
	line := Event{Timestamp: ts, ProjectID: "p", Service: "shop", Env: "prod", Release: "1.4", DurationMS: 35,
		HTTP: &HTTPInfo{Method: "post", Route: "/orders", Status: 201}, User: &UserInfo{IDHash: "u1"}}
	c, ok := CountOf(line)
	if !ok || c.Requests != 1 || c.Errors != 0 || c.Method != "POST" || c.Release != "1.4" || len(c.DurationsMS) != 1 || c.DurationsMS[0] != 35 || len(c.Users) != 1 || c.Minute.Second() != 0 {
		t.Fatalf("request line: %+v %v", c, ok)
	}

	crash := Event{Timestamp: ts, HTTP: &HTTPInfo{Method: "GET", Route: "/boom", Status: 500}, Exception: &Exception{Type: "ValueError"}}
	if c, ok := CountOf(crash); !ok || c.Errors != 1 || len(c.DurationsMS) != 0 || len(c.Users) != 0 {
		t.Fatalf("a crashed request counts as a failure without a response time: %+v %v", c, ok)
	}

	for name, ev := range map[string]Event{
		"plain log":               {Timestamp: ts, Message: "hello"},
		"in a request, no timing": {Timestamp: ts, HTTP: &HTTPInfo{Method: "GET", Route: "/", Status: 200}},
		"handled error in a 200":  {Timestamp: ts, HTTP: &HTTPInfo{Method: "GET", Route: "/", Status: 200}, Exception: &Exception{Type: "KeyError"}},
		"no route":                {Timestamp: ts, DurationMS: 3, HTTP: &HTTPInfo{Method: "GET", Status: 200}},
	} {
		if _, ok := CountOf(ev); ok {
			t.Errorf("%s: is not a request", name)
		}
	}
}
