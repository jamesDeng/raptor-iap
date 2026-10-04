package execution

import (
	"context"
	"encoding/json"
	"regexp"
)

var skillsTag = regexp.MustCompile(`^skills-v[0-9]+\.[0-9]+\.[0-9]+$`)
var skillsSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)

func (w *Worker) ApplySkillsChange(ctx context.Context, x Execution, signal Signal) error {
	var in struct {
		Version  SkillsVersion `json:"version"`
		Strategy string        `json:"strategy"`
	}
	if json.Unmarshal(signal.Payload, &in) != nil || !skillsTag.MatchString(in.Version.Tag) || !skillsSHA.MatchString(in.Version.CommitSHA) || (in.Strategy != "interrupt" && in.Strategy != "next-pause") {
		return ErrInvalid
	}
	if x.Status == "completed" || x.Status == "cancelled" {
		return nil
	}
	b, _ := json.Marshal(in.Version)
	if _, e := w.Store.Pool.Exec(ctx, "UPDATE gateway.executions SET pending_skills=$2 WHERE request_id=$1", x.RequestID, b); e != nil {
		return e
	}
	if in.Strategy == "next-pause" && x.Status != "waiting_approval" && x.Status != "waiting_review" {
		return nil
	}
	var held *string
	if e := w.Store.Pool.QueryRow(ctx, "SELECT request_id::text FROM gateway.runtime_slot WHERE id=1").Scan(&held); e != nil {
		return e
	}
	if held != nil && *held == x.RequestID {
		if e := w.Pause(ctx, x, "interrupted"); e != nil {
			return e
		}
	}
	fresh, e := w.Store.Get(ctx, x.RequestID)
	if e != nil {
		return e
	}
	if fresh.RecoveryNeeded {
		return ErrUnavailable
	}
	if e = w.applyPendingSkills(ctx, x.RequestID); e != nil {
		return e
	}
	if in.Strategy == "interrupt" {
		_, e = w.Store.Pool.Exec(ctx, "UPDATE gateway.executions SET status='queued',queued_at=now() WHERE request_id=$1", x.RequestID)
	}
	return e
}
func (w *Worker) applyPendingSkills(ctx context.Context, id string) error {
	_, e := w.Store.Pool.Exec(ctx, "UPDATE gateway.executions SET applied_skills=pending_skills,pending_skills='{}' WHERE request_id=$1 AND pending_skills<>'{}'", id)
	return e
}
