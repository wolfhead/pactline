package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"bountyboard/internal/application"
	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type projectHandler struct {
	service  *application.ProjectService
	projects *store.ProjectStore
}

func parseProjectNumber(w http.ResponseWriter, r *http.Request) (int64, bool) {
	number, err := strconv.ParseInt(r.PathValue("number"), 10, 64)
	if err != nil || number <= 0 {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "project number must be a positive integer"})
		return 0, false
	}
	return number, true
}

func parseDateField(field, raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be YYYY-MM-DD", domain.ErrInvalidInput, field)
	}
	return &value, nil
}

type createProjectRequest struct {
	Name        string    `json:"name"`
	Outcome     string    `json:"outcome"`
	Description string    `json:"description"`
	OwnerID     uuid.UUID `json:"owner_id"`
	TargetDate  *string   `json:"target_date"`
}

func (h *projectHandler) create(w http.ResponseWriter, r *http.Request) {
	var request createProjectRequest
	if !DecodeBody(w, r, &request) {
		return
	}
	var targetDate *time.Time
	var err error
	if request.TargetDate != nil {
		targetDate, err = parseDateField("target_date", *request.TargetDate)
		if err != nil {
			WriteError(w, r, err)
			return
		}
	}
	project, err := h.projects.Create(r.Context(), domain.Project{
		Name: request.Name, Outcome: request.Outcome, Description: request.Description,
		OwnerID: request.OwnerID, CreatorID: CurrentUser(r).ID,
		Status: domain.ProjectStatusPlanned, TargetDate: targetDate,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, newProjectView(project))
}

func (h *projectHandler) list(w http.ResponseWriter, r *http.Request) {
	projects, err := h.projects.List(r.Context(), r.URL.Query().Get("archived") == "all")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	views := make([]projectView, len(projects))
	for i, project := range projects {
		views[i] = newProjectView(project)
	}
	WriteJSON(w, http.StatusOK, views)
}

func (h *projectHandler) get(w http.ResponseWriter, r *http.Request) {
	number, ok := parseProjectNumber(w, r)
	if !ok {
		return
	}
	detail, err := h.service.GetDetail(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newProjectDetailView(detail))
}

func (h *projectHandler) update(w http.ResponseWriter, r *http.Request) {
	number, ok := parseProjectNumber(w, r)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if !DecodeBody(w, r, &raw) {
		return
	}
	var patch store.ProjectPatch
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
	if value, exists := raw["owner_id"]; exists {
		var parsed uuid.UUID
		if err := json.Unmarshal(value, &parsed); err != nil {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "owner_id is malformed"})
			return
		}
		patch.OwnerID = &parsed
	}
	if value, exists := raw["target_date"]; exists {
		patch.TargetDateSet = true
		if !isJSONNull(value) {
			var parsed string
			if err := json.Unmarshal(value, &parsed); err != nil {
				WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "target_date is malformed"})
				return
			}
			targetDate, err := parseDateField("target_date", parsed)
			if err != nil {
				WriteError(w, r, err)
				return
			}
			patch.TargetDate = targetDate
		}
	}
	project, err := h.projects.Update(r.Context(), number, patch, CurrentUser(r).ID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newProjectView(project))
}

type lifecycleRequest struct {
	Reason string `json:"reason"`
}

func (h *projectHandler) lifecycle(action store.ProjectLifecycleAction) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		number, ok := parseProjectNumber(w, r)
		if !ok {
			return
		}
		var request lifecycleRequest
		if r.Body != nil && r.ContentLength != 0 && !DecodeBody(w, r, &request) {
			return
		}
		userID := CurrentUser(r).ID
		project, err := h.service.ApplyProjectLifecycle(r.Context(), number, action,
			domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}, request.Reason)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, newProjectView(project))
	}
}
