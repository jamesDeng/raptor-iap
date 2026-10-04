package github

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
	"strings"
)

type delivery struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest struct {
		Number int  `json:"number"`
		Merged bool `json:"merged"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Review struct {
		State    string `json:"state"`
		Body     string `json:"body"`
		CommitID string `json:"commit_id"`
	} `json:"review"`
}

func (s *Service) HandleDelivery(ctx context.Context, id, event string, body []byte) error {
	if id == "" || len(id) > 200 || event == "" || len(body) > 262144 || !json.Valid(body) {
		return domain.ErrInvalid
	}
	var v delivery
	if json.Unmarshal(body, &v) != nil {
		return domain.ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, "INSERT INTO raptor.webhook_deliveries(id,event_type,payload) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", id, event, body)
	if e != nil {
		return domain.ErrUnavailable
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	kind := ""
	if event == "pull_request_review" && v.Action == "submitted" && (v.Review.State == "changes_requested" || v.Review.State == "commented") {
		kind = "review"
	}
	if event == "pull_request" && v.Action == "closed" && v.PullRequest.Merged {
		kind = "merged"
	}
	if kind != "" {
		var requestID, status, head string
		e = tx.QueryRow(ctx, "SELECT r.id::text,r.status,p.head_sha FROM raptor.pull_requests p JOIN raptor.requests r ON r.id=p.request_id WHERE p.repository=$1 AND p.number=$2 FOR UPDATE OF r", strings.ToLower(v.Repository.FullName), v.PullRequest.Number).Scan(&requestID, &status, &head)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return domain.ErrUnavailable
		}
		if e == nil && status != "cancelled" && status != "completed" && (kind == "merged" || v.Review.CommitID == head) {
			if e = requests.QueueSignal(ctx, tx, requestID, kind, map[string]any{"repository": v.Repository.FullName, "number": v.PullRequest.Number, "headSha": v.PullRequest.Head.SHA, "reviewState": v.Review.State, "instructions": v.Review.Body}); e != nil {
				return domain.ErrUnavailable
			}
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE raptor.webhook_deliveries SET processed=true WHERE id=$1", id); e != nil {
		return domain.ErrUnavailable
	}
	return tx.Commit(ctx)
}
