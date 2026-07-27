package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	sharedapi "bountyboard/internal/api"
	userdomain "bountyboard/internal/domain"
	"bountyboard/internal/legacy/domain"
	"bountyboard/internal/legacy/scoring"
	"bountyboard/internal/legacy/store"

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
		sharedapi.WriteJSON(w, http.StatusBadRequest, sharedapi.ErrorBody{Error: "id is not a UUID"})
		return
	}
	var req setValueLevelRequest
	if !sharedapi.DecodeBody(w, r, &req) {
		return
	}
	if !domain.IsValidValueLevel(req.ValueLevel) {
		sharedapi.WriteJSON(w, http.StatusBadRequest, sharedapi.ErrorBody{Error: "value_level is not a known value: " + string(req.ValueLevel)})
		return
	}

	b, err := h.bounties.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	me := sharedapi.CurrentUser(r)
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
	sharedapi.WriteJSON(w, http.StatusOK, updated)
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
		sharedapi.WriteJSON(w, http.StatusBadRequest, sharedapi.ErrorBody{Error: "id is not a UUID"})
		return
	}
	var req setDifficultyRequest
	if !sharedapi.DecodeBody(w, r, &req) {
		return
	}
	if !domain.IsValidDifficulty(req.Difficulty) {
		sharedapi.WriteJSON(w, http.StatusBadRequest, sharedapi.ErrorBody{Error: "difficulty is not a known value: " + string(req.Difficulty)})
		return
	}

	me := sharedapi.CurrentUser(r)
	b, err := h.bounties.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	// CanSetDifficulty needs the bounty (not just the actor) since I2: it also
	// refuses once the bounty has been settled, so the fetch above must
	// happen before this check now, not after it.
	if err := domain.CanSetDifficulty(me, b); err != nil {
		slog.Warn("set difficulty denied",
			"bounty_id", id, "actor_id", me.ID, "actor_roles", me.Roles,
			"settled_at", b.SettledAt, "error", err)
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
	sharedapi.WriteJSON(w, http.StatusOK, updated)
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

// failedItem is a bounty that was a candidate for settlement but whose write
// genuinely failed for a reason other than "someone already settled it" (I1)
// — e.g. an infrastructure or database error. This bucket exists specifically
// so these are never folded into AlreadySettledCount, which is the benign,
// expected outcome of a re-run.
type failedItem struct {
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
	Failed              []failedItem     `json:"failed"`
	FailedCount         int              `json:"failed_count"`
}

// settle is the steward-invoked endpoint required by spec §7.2 and the task
// brief's §3: for every terminal bounty whose completed_at falls in the
// half-open period [from, to), compute and snapshot its score exactly once.
// The period is half-open, not inclusive at both ends, so that adjacent
// settlement runs (e.g. back-to-back months) do not both claim the boundary
// instant.
//
// Already-settled records are skipped and counted, not recomputed — spec
// §7.2 requires reading a settled score to always read the snapshot, so a
// botched run can simply be re-run without disturbing what already
// committed. A record missing a level it needs is unscorable: it is skipped,
// logged with its full input state, and reported in the response — never
// given an invented default, since a grade nobody gave would be fiction in
// the archive.
//
// A genuine write failure (I1) is its own bucket (Failed/FailedCount), never
// folded into AlreadySettledCount: only a Settle error satisfying
// errors.Is(err, domain.ErrAlreadySettled) — meaning the database's own
// "WHERE settled_at IS NULL" guard found nothing to update because another
// run already settled this row — belongs in that count. Anything else (a
// dropped connection, a constraint violation, a context cancellation) is a
// real failure the steward must be told about, not a silently-swallowed skip
// that makes an infrastructure outage look like ordinary idempotency.
func (h *settlementHandler) settle(w http.ResponseWriter, r *http.Request) {
	me := sharedapi.CurrentUser(r)
	if !me.HasRole(userdomain.UserRoleSteward) {
		slog.Warn("settlement denied", "actor_id", me.ID, "actor_roles", me.Roles)
		writeError(w, r, domain.ErrForbidden)
		return
	}
	var req settleRequest
	if !sharedapi.DecodeBody(w, r, &req) {
		return
	}
	if req.From.IsZero() || req.To.IsZero() {
		sharedapi.WriteJSON(w, http.StatusBadRequest, sharedapi.ErrorBody{Error: "from and to are both required"})
		return
	}
	if req.To.Before(req.From) {
		sharedapi.WriteJSON(w, http.StatusBadRequest, sharedapi.ErrorBody{Error: "to must not be before from"})
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

	resp := settlementResponse{Settled: []settledItem{}, Unscorable: []unscorableItem{}, Failed: []failedItem{}}
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
			if isAlreadySettledError(err) {
				// A concurrent settlement run beat this one to the same row.
				// This is the one genuinely benign Settle failure — the
				// database's own "WHERE settled_at IS NULL" guard is what
				// produced it — so it belongs in AlreadySettledCount exactly
				// like the pre-check skip above.
				slog.Info("settlement skip: already settled (race)", "bounty_id", b.ID, "error", err)
				resp.AlreadySettledCount++
				continue
			}
			// I1: anything else is a real failure — a dropped connection, a
			// constraint violation, a cancelled context — and must not be
			// folded into already_settled_count. That count is the report a
			// steward reads to confirm a re-run is safe; hiding an
			// infrastructure failure inside it would make the report false.
			slog.Error("settlement write failed", "bounty_id", b.ID, "error", err)
			resp.Failed = append(resp.Failed, failedItem{BountyID: b.ID, Title: b.Title, Reason: err.Error()})
			resp.FailedCount++
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
		"unscorable_count", resp.UnscorableCount, "failed_count", resp.FailedCount)
	sharedapi.WriteJSON(w, http.StatusOK, resp)
}

// isAlreadySettledError is the I1 classification: the ONLY Settle failure
// that belongs in AlreadySettledCount is one that satisfies
// errors.Is(err, domain.ErrAlreadySettled) — the database's own
// "WHERE settled_at IS NULL" guard finding nothing to update because another
// run already settled the row (BountyStore.Settle translates the resulting
// pgx.ErrNoRows into domain.ErrAlreadySettled itself). Every other error
// (a dropped connection, a constraint violation, a cancelled context) must
// fall through to the Failed bucket instead. Pulled out as its own function
// so the branch this settlement run's report correctness hinges on can be
// pinned by a direct unit test against synthetic errors, independent of
// whether a real infrastructure failure can be reproduced deterministically
// in an integration test.
func isAlreadySettledError(err error) bool {
	return errors.Is(err, domain.ErrAlreadySettled)
}
