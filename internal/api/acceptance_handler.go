package api

import (
	"net/http"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type acceptanceHandler struct {
	tasks      *store.TaskStore
	projects   *store.ProjectStore
	milestones *store.MilestoneStore
	acceptance *store.AcceptanceStore
}

type criterionRequest struct {
	Criterion                string `json:"criterion"`
	VerificationInstructions string `json:"verification_instructions"`
	Position                 int    `json:"position"`
}

func (h *acceptanceHandler) listProject(w http.ResponseWriter, r *http.Request) {
	number, ok := parseProjectNumber(w, r)
	if !ok {
		return
	}
	projectID, err := h.projects.ResolveProjectID(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	criteria, err := h.acceptance.ListForProject(r.Context(), projectID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newCriterionViews(criteria))
}

func (h *acceptanceHandler) createProject(w http.ResponseWriter, r *http.Request) {
	number, ok := parseProjectNumber(w, r)
	if !ok {
		return
	}
	var request criterionRequest
	if !DecodeBody(w, r, &request) {
		return
	}
	projectID, err := h.projects.ResolveProjectID(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	criterion, err := h.acceptance.CreateWithOperation(r.Context(), domain.AcceptanceCriterion{
		ProjectID: &projectID, Criterion: request.Criterion,
		VerificationInstructions: request.VerificationInstructions,
		Revision:                 1, Position: request.Position,
	}, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, newCriterionView(store.CriterionWithCurrentCheck{Criterion: criterion}))
}

func (h *acceptanceHandler) milestoneOwner(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	number, ok := parseProjectNumber(w, r)
	if !ok {
		return uuid.Nil, false
	}
	milestoneID, ok := parseMilestoneID(w, r)
	if !ok {
		return uuid.Nil, false
	}
	projectID, err := h.projects.ResolveProjectID(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return uuid.Nil, false
	}
	belongs, err := h.milestones.BelongsToProject(r.Context(), milestoneID, projectID)
	if err != nil {
		WriteError(w, r, err)
		return uuid.Nil, false
	}
	if !belongs {
		WriteError(w, r, domain.ErrNotFound)
		return uuid.Nil, false
	}
	return milestoneID, true
}

func (h *acceptanceHandler) listMilestone(w http.ResponseWriter, r *http.Request) {
	milestoneID, ok := h.milestoneOwner(w, r)
	if !ok {
		return
	}
	criteria, err := h.acceptance.ListForMilestone(r.Context(), milestoneID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newCriterionViews(criteria))
}

func (h *acceptanceHandler) createMilestone(w http.ResponseWriter, r *http.Request) {
	milestoneID, ok := h.milestoneOwner(w, r)
	if !ok {
		return
	}
	var request criterionRequest
	if !DecodeBody(w, r, &request) {
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	criterion, err := h.acceptance.CreateWithOperation(r.Context(), domain.AcceptanceCriterion{
		MilestoneID: &milestoneID, Criterion: request.Criterion,
		VerificationInstructions: request.VerificationInstructions,
		Revision:                 1, Position: request.Position,
	}, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, newCriterionView(store.CriterionWithCurrentCheck{Criterion: criterion}))
}

func (h *acceptanceHandler) taskOwner(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	number, ok := parseTaskNumber(w, r)
	if !ok {
		return uuid.Nil, false
	}
	task, err := h.tasks.GetByNumber(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return uuid.Nil, false
	}
	return task.Task.ID, true
}

func (h *acceptanceHandler) listTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskOwner(w, r)
	if !ok {
		return
	}
	criteria, err := h.acceptance.ListForTask(r.Context(), taskID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newCriterionViews(criteria))
}

func (h *acceptanceHandler) createTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskOwner(w, r)
	if !ok {
		return
	}
	var request criterionRequest
	if !DecodeBody(w, r, &request) {
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	criterion, err := h.acceptance.CreateWithOperation(r.Context(), domain.AcceptanceCriterion{
		TaskID: &taskID, Criterion: request.Criterion,
		VerificationInstructions: request.VerificationInstructions,
		Revision:                 1, Position: request.Position,
	}, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, newCriterionView(store.CriterionWithCurrentCheck{Criterion: criterion}))
}

type updateCriterionRequest struct {
	Criterion                *string `json:"criterion"`
	VerificationInstructions *string `json:"verification_instructions"`
	Position                 *int    `json:"position"`
}

func (h *acceptanceHandler) update(w http.ResponseWriter, r *http.Request) {
	criterionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "criterion id must be a UUID"})
		return
	}
	var request updateCriterionRequest
	if !DecodeBody(w, r, &request) {
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	criterion, err := h.acceptance.UpdateWithOperation(
		r.Context(), criterionID, request.Criterion,
		request.VerificationInstructions, request.Position, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newCriterionView(store.CriterionWithCurrentCheck{Criterion: criterion}))
}

type createCheckRequest struct {
	CriterionRevision int                      `json:"criterion_revision"`
	Outcome           domain.AcceptanceOutcome `json:"outcome"`
	Evidence          string                   `json:"evidence"`
}

func (h *acceptanceHandler) check(w http.ResponseWriter, r *http.Request) {
	criterionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "criterion id must be a UUID"})
		return
	}
	var request createCheckRequest
	if !DecodeBody(w, r, &request) {
		return
	}
	userID := CurrentUser(r).ID
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	check, err := h.acceptance.AddCheckWithOperation(r.Context(), domain.AcceptanceCheck{
		CriterionID: criterionID, CriterionRevision: request.CriterionRevision,
		Outcome: request.Outcome, Evidence: request.Evidence,
		Checker: domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
	}, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, acceptanceCheckView{
		ID: check.ID, CriterionRevision: check.CriterionRevision,
		Outcome: check.Outcome, Evidence: check.Evidence,
		CheckerType: check.Checker.Type, CheckedByUserID: check.Checker.UserID,
		CheckerRef: check.Checker.Ref, CheckedAt: check.CheckedAt,
	})
}

func (h *acceptanceHandler) remove(w http.ResponseWriter, r *http.Request) {
	criterionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "criterion id must be a UUID"})
		return
	}
	var request lifecycleRequest
	if r.Body != nil && r.ContentLength != 0 && !DecodeBody(w, r, &request) {
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	if err := h.acceptance.RemoveCriterionWithOperation(
		r.Context(), criterionID, actor, request.Reason,
	); err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusNoContent, nil)
}
