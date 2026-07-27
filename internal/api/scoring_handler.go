package api

import (
	"log/slog"
	"net/http"
	"time"

	"bountyboard/internal/domain"
	"bountyboard/internal/scoring"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type setValueLevelRequest struct {
	ValueLevel domain.ValueLevel `json:"value_level"`
}

// setValueLevel is the sponsor's (or a steward's) dedicated channel to set or
// amend a bounty's value level, per spec §6.1. Authorisation and the
// DRAFT/OPEN window are both enforced by domain.CanSetValueLevel — see its
// doc comment for why the window exists.
func (h *bountyHandler) setValueLevel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	var req setValueLevelRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !domain.IsValidValueLevel(req.ValueLevel) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "value_level is not a known value: " + string(req.ValueLevel)})
		return
	}

	b, err := h.bounties.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	me := CurrentUser(r)
	if err := domain.CanSetValueLevel(me, b); err != nil {
		slog.Warn("set value level denied",
			"bounty_id", id, "actor_id", me.ID, "actor_roles", me.Roles, "status", b.Status, "error", err)
		writeError(w, r, err)
		return
	}

	before := b.ValueLevel
	b.ValueLevel = req.ValueLevel
	updated, err := h.bounties.Update(r.Context(), b)
	if err != nil {
		writeError(w, r, err)
		return
	}
	slog.Info("value level set",
		"bounty_id", updated.ID, "actor_id", me.ID, "value_level_before", before, "value_level_after", updated.ValueLevel)
	writeJSON(w, http.StatusOK, updated)
}

type setDifficultyRequest struct {
	Difficulty domain.Difficulty `json:"difficulty"`
}

// setDifficulty is the TECH_LEAD/STEWARD-only channel to set a bounty's
// difficulty. Deliberately not reachable by the sponsor, even for their own
// bounty — see domain.CanSetDifficulty's doc comment for why.
func (h *bountyHandler) setDifficulty(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	var req setDifficultyRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !domain.IsValidDifficulty(req.Difficulty) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "difficulty is not a known value: " + string(req.Difficulty)})
		return
	}

	me := CurrentUser(r)
	if err := domain.CanSetDifficulty(me); err != nil {
		slog.Warn("set difficulty denied",
			"bounty_id", id, "actor_id", me.ID, "actor_roles", me.Roles, "error", err)
		writeError(w, r, err)
		return
	}

	b, err := h.bounties.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}

	before := b.Difficulty
	b.Difficulty = req.Difficulty
	updated, err := h.bounties.Update(r.Context(), b)
	if err != nil {
		writeError(w, r, err)
		return
	}
	slog.Info("difficulty set",
		"bounty_id", updated.ID, "actor_id", me.ID, "difficulty_before", before, "difficulty_after", updated.Difficulty)
	writeJSON(w, http.StatusOK, updated)
}

// settlementHandler runs spec §7's monthly settlement: computing and
// snapshotting scores for every terminal bounty in a period.
type settlementHandler struct {
	bounties *store.BountyStore
}

type settleRequest struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// settledItem and unscorableItem are the two outcomes a settlement run
// reports per record, alongside the count of records it skipped because they
// were already settled — see settlementResponse.
type settledItem struct {
	BountyID uuid.UUID `json:"bounty_id"`
	Title    string    `json:"title"`
	Score    float64   `json:"score"`
}

type unscorableItem struct {
	BountyID uuid.UUID `json:"bounty_id"`
	Title    string    `json:"title"`
	Reason   string    `json:"reason"`
}

type settlementResponse struct {
	Settled             []settledItem    `json:"settled"`
	SettledCount        int              `json:"settled_count"`
	AlreadySettledCount int              `json:"already_settled_count"`
	Unscorable          []unscorableItem `json:"unscorable"`
	UnscorableCount     int              `json:"unscorable_count"`
}

// settle is the steward-invoked endpoint required by spec §7.2 and the task
// brief's §3: for every terminal bounty whose completed_at falls in
// [from, to], compute and snapshot its score exactly once.
//
// Already-settled records are skipped and counted, not recomputed — spec
// §7.2 requires reading a settled score to always read the snapshot, so a
// botched run can simply be re-run without disturbing what already
// committed. A record missing a level it needs is unscorable: it is skipped,
// logged with its full input state, and reported in the response — never
// given an invented default, since a grade nobody gave would be fiction in
// the archive.
func (h *settlementHandler) settle(w http.ResponseWriter, r *http.Request) {
	me := CurrentUser(r)
	if !me.HasRole(domain.UserRoleSteward) {
		slog.Warn("settlement denied", "actor_id", me.ID, "actor_roles", me.Roles)
		writeError(w, r, domain.ErrForbidden)
		return
	}
	var req settleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.From.IsZero() || req.To.IsZero() {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "from and to are both required"})
		return
	}
	if req.To.Before(req.From) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "to must not be before from"})
		return
	}

	list, err := h.bounties.List(r.Context(), store.BountyFilter{
		Statuses:      []domain.Status{domain.StatusCompleted, domain.StatusAbandoned},
		CompletedFrom: &req.From,
		CompletedTo:   &req.To,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	resp := settlementResponse{Settled: []settledItem{}, Unscorable: []unscorableItem{}}
	settledAt := time.Now().UTC()
	for _, b := range list {
		if b.SettledAt != nil {
			resp.AlreadySettledCount++
			slog.Info("settlement skip: already settled",
				"bounty_id", b.ID, "settled_score", b.SettledScore, "settled_at", b.SettledAt)
			continue
		}

		score, err := scoring.Score(b)
		if err != nil {
			resp.Unscorable = append(resp.Unscorable, unscorableItem{BountyID: b.ID, Title: b.Title, Reason: err.Error()})
			resp.UnscorableCount++
			slog.Warn("settlement skip: unscorable",
				"bounty_id", b.ID, "status", b.Status, "value_level", b.ValueLevel,
				"difficulty", b.Difficulty, "completion", b.Completion, "commitment", b.Commitment,
				"reason", err.Error())
			continue
		}

		updated, err := h.bounties.Settle(r.Context(), b.ID, score, settledAt)
		if err != nil {
			// A concurrent settlement run beat this one to the same row —
			// surfaced loudly rather than silently dropped, since it changes
			// the already_settled_count the caller sees.
			slog.Error("settlement write failed", "bounty_id", b.ID, "error", err)
			resp.AlreadySettledCount++
			continue
		}
		resp.Settled = append(resp.Settled, settledItem{BountyID: updated.ID, Title: updated.Title, Score: score})
		resp.SettledCount++
		slog.Info("settlement input/output",
			"bounty_id", updated.ID, "status", updated.Status, "value_level", updated.ValueLevel,
			"difficulty", updated.Difficulty, "completion", updated.Completion, "commitment", updated.Commitment,
			"score", score)
	}

	slog.Info("settlement run complete",
		"actor_id", me.ID, "from", req.From, "to", req.To,
		"settled_count", resp.SettledCount, "already_settled_count", resp.AlreadySettledCount,
		"unscorable_count", resp.UnscorableCount)
	writeJSON(w, http.StatusOK, resp)
}
