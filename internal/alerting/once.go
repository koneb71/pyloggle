package alerting

import (
	"context"
	"log"
	"time"

	"pyloggle/internal/api"
)

// Each rule sends a thing once. What was sent is recorded per rule and subject
// (alert_notified):
//
//   - new problem: "issue:<id>", never again for that rule;
//   - came back:   "came_back:<activity id>", once per comeback;
//   - jumped:      "jump:<id>", at most once a day;
//   - count rules: "incident", sent when it starts; quiet while it lasts; closed (without a
//     message) when a check finds it back to normal, so the next one alerts again;
//   - recap:       "recap:<local date>", once a day.

const incidentSubject = "incident"

func issueSubject(id string) string    { return "issue:" + id }
func cameBackSubject(id string) string { return "came_back:" + id }
func jumpSubject(id string) string     { return "jump:" + id }
func recapSubject(day string) string   { return "recap:" + day }

// unsent drops the items the rule already sent (since the given time; zero means ever). When
// the record cannot be read, everything is kept: a repeated alert beats a missed one.
func unsent[T any](ctx context.Context, r *Runner, rule api.AlertRule, since time.Time, items []T, subject func(T) string) []T {
	if len(items) == 0 {
		return items
	}
	subjects := make([]string, len(items))
	for i, it := range items {
		subjects[i] = subject(it)
	}
	sent, err := r.store.NotifiedSubjects(ctx, rule.ProjectID, rule.ID, subjects, since)
	if err != nil {
		log.Printf("alerting: rule=%s: could not read what was already sent: %v", rule.ID, err)
		return items
	}
	out := items[:0:0]
	for i, it := range items {
		if !sent[subjects[i]] {
			out = append(out, it)
		}
	}
	return out
}

// incidentStarts decides whether a count rule sends now: only when its condition holds and no
// incident is open. A check that finds the condition gone closes the open incident quietly.
func (r *Runner) incidentStarts(ctx context.Context, rule api.AlertRule, holds bool) bool {
	open, err := r.store.IncidentOpen(ctx, rule.ProjectID, rule.ID)
	if err != nil {
		log.Printf("alerting: rule=%s: could not read the incident state: %v", rule.ID, err)
		return holds
	}
	switch {
	case holds && open:
		return false // still going on: already sent
	case !holds && open:
		if err := r.store.MarkNotified(ctx, rule.ProjectID, rule.ID, []string{incidentSubject}, false); err != nil {
			log.Printf("alerting: rule=%s: could not close the incident: %v", rule.ID, err)
		}
		return false
	}
	return holds
}
