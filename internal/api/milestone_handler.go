package api

import (
	"encoding/json"
	"net/http"
	"time"

	"bountyboard/internal/application"
	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type milestoneHandler struct {
	service    *application.ProjectService
	projects   *store.ProjectStore
	milestones *store.MilestoneStore
}

func parseMilestoneID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "milestone id must be a UUID"})
		return uuid.Nil, false
	}
	return id, true
}

type milestoneRequest struct {
	Name        string  `json:"name"`
	Outcome     string  `json:"outcome"`
	Description string  `json:"description"`
	TargetDate  *string `json:"target_date"`
	Position    int     `json:"position"`
}

func (h *milestoneHandler) create(w http.ResponseWriter, r *http.Request) {
	number, ok := parseProjectNumber(w, r)
	if !ok {
		return
	}
	var request milestoneRequest
	if !DecodeBody(w, r, &request) {
		return
	}
	projectID, err := h.projects.ResolveProjectID(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var targetDate *time.Time
	if request.TargetDate != nil {
		targetDate, err = parseDateField("target_date", *request.TargetDate)
		if err != nil {
			WriteError(w, r, err)
			return
		}
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	milestone, err := h.milestones.CreateWithOperation(r.Context(), domain.Milestone{
		ProjectID: projectID, Name: request.Name, Outcome: request.Outcome,
		Description: request.Description, Status: domain.MilestoneStatusOpen,
		TargetDate: targetDate, Position: request.Position,
	}, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, newMilestoneView(milestone, nil))
}

func (h *milestoneHandler) update(w http.ResponseWriter, r *http.Request) {
	number, ok := parseProjectNumber(w, r)
	if !ok {
		return
	}
	milestoneID, ok := parseMilestoneID(w, r)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if !DecodeBody(w, r, &raw) {
		return
	}
	projectID, err := h.projects.ResolveProjectID(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var patch store.MilestonePatch
	if value, exists := raw["name"]; exists {
		var parsed string
		if err := json.Unmarshal(value, &parsed); err != nil {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "name is malformed"})
			return
		}
		patch.Name = &parsed
	}
	if value, exists := raw["outcome"]; exists {
		var parsed string
		if err := json.Unmarshal(value, &parsed); err != nil {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "outcome is malformed"})
			return
		}
		patch.Outcome = &parsed
	}
	if value, exists := raw["description"]; exists {
		var parsed string
		if err := json.Unmarshal(value, &parsed); err != nil {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "description is malformed"})
			return
		}
		patch.Description = &parsed
	}
	if value, exists := raw["position"]; exists {
		var parsed int
		if err := json.Unmarshal(value, &parsed); err != nil {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "position is malformed"})
			return
		}
		patch.Position = &parsed
	}
	if value, exists := raw["target_date"]; exists {
		patch.TargetDateSet = true
		if !isJSONNull(value) {
			var parsed string
			if err := json.Unmarshal(value, &parsed); err != nil {
				WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "target_date is malformed"})
				return
			}
			patch.TargetDate, err = parseDateField("target_date", parsed)
			if err != nil {
				WriteError(w, r, err)
				return
			}
		}
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	milestone, err := h.milestones.UpdateWithOperation(
		r.Context(), projectID, milestoneID, patch, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newMilestoneView(milestone, nil))
}

func (h *milestoneHandler) lifecycle(action store.MilestoneLifecycleAction) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		number, ok := parseProjectNumber(w, r)
		if !ok {
			return
		}
		milestoneID, ok := parseMilestoneID(w, r)
		if !ok {
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
		milestone, err := h.service.ApplyMilestoneLifecycleWithOperation(
			r.Context(), number, milestoneID, action, actor, request.Reason)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, newMilestoneView(milestone, nil))
	}
}
