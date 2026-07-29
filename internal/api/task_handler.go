package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"bountyboard/internal/application"
	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type taskHandler struct {
	tasks    *store.TaskStore
	projects *application.ProjectService
}

// parseTaskNumber reads the {number} path value, writing a 400 and
// returning false if it is not a positive integer. Tasks are addressed by
// their short, human-facing sequential number ("look at 142"), not their
// UUID, everywhere in this API.
func parseTaskNumber(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("number")
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "number must be a positive integer"})
		return 0, false
	}
	return n, true
}

func parseDueDate(w http.ResponseWriter, raw string) (time.Time, bool) {
	d, err := time.Parse("2006-01-02", raw)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "due_date must be YYYY-MM-DD"})
		return time.Time{}, false
	}
	return d, true
}

type createTaskRequest struct {
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	Status        string      `json:"status"`
	Priority      string      `json:"priority"`
	AssigneeID    *uuid.UUID  `json:"assignee_id"`
	DueDate       *string     `json:"due_date"`
	LabelIDs      []uuid.UUID `json:"label_ids"`
	ProjectNumber *int64      `json:"project_number"`
	MilestoneID   *uuid.UUID  `json:"milestone_id"`
}

func (h *taskHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if !DecodeBody(w, r, &req) {
		return
	}
	me := CurrentUser(r)

	var dueDate *time.Time
	if req.DueDate != nil {
		d, ok := parseDueDate(w, *req.DueDate)
		if !ok {
			return
		}
		dueDate = &d
	}
	projectID, milestoneID, err := h.projects.ResolveTaskAssociation(r.Context(), req.ProjectNumber, req.MilestoneID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}

	out, err := h.tasks.CreateWithOperation(r.Context(), domain.Task{
		Title:       req.Title,
		Description: req.Description,
		Status:      domain.TaskStatus(req.Status),
		Priority:    domain.TaskPriority(req.Priority),
		AssigneeID:  req.AssigneeID,
		CreatorID:   me.ID,
		DueDate:     dueDate,
		ProjectID:   projectID,
		MilestoneID: milestoneID,
	}, req.LabelIDs, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, newTaskView(out))
}

func (h *taskHandler) get(w http.ResponseWriter, r *http.Request) {
	number, ok := parseTaskNumber(w, r)
	if !ok {
		return
	}
	out, err := h.tasks.GetByNumber(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newTaskView(out))
}

// decodeTaskPatch reads a PATCH body into a domain.TaskPatch, distinguishing
// a field that is absent (left unchanged) from one explicitly set to JSON
// null (cleared) for the nullable fields — a plain json.Decode into a
// pointer-typed struct cannot make that distinction, since it sets a *T
// field to nil for both cases. Decoding into a map of raw messages first,
// and checking key presence, is what lets update-any-field PATCH semantics
// actually mean "any field, including unsetting the nullable ones".
type decodedTaskPatch struct {
	Patch            domain.TaskPatch
	ProjectNumberSet bool
	ProjectNumber    *int64
	MilestoneSet     bool
	MilestoneID      *uuid.UUID
}

func decodeTaskPatch(w http.ResponseWriter, r *http.Request) (decodedTaskPatch, bool) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		slog.Warn("decode task patch body", "path", r.URL.Path, "error", err)
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid JSON body"})
		return decodedTaskPatch{}, false
	}

	var patch domain.TaskPatch
	var decoded decodedTaskPatch
	badField := func(field string) {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: field + " is malformed"})
	}

	if v, ok := raw["title"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			badField("title")
			return decodedTaskPatch{}, false
		}
		patch.Title = &s
	}
	if v, ok := raw["description"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			badField("description")
			return decodedTaskPatch{}, false
		}
		patch.Description = &s
	}
	if v, ok := raw["status"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			badField("status")
			return decodedTaskPatch{}, false
		}
		st := domain.TaskStatus(s)
		patch.Status = &st
	}
	if v, ok := raw["priority"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			badField("priority")
			return decodedTaskPatch{}, false
		}
		p := domain.TaskPriority(s)
		patch.Priority = &p
	}
	if v, ok := raw["assignee_id"]; ok {
		patch.AssigneeSet = true
		if !isJSONNull(v) {
			var id uuid.UUID
			if err := json.Unmarshal(v, &id); err != nil {
				badField("assignee_id")
				return decodedTaskPatch{}, false
			}
			patch.AssigneeID = &id
		}
	}
	if v, ok := raw["due_date"]; ok {
		patch.DueDateSet = true
		if !isJSONNull(v) {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				badField("due_date")
				return decodedTaskPatch{}, false
			}
			d, ok := parseDueDate(w, s)
			if !ok {
				return decodedTaskPatch{}, false
			}
			patch.DueDate = &d
		}
	}
	if v, ok := raw["label_ids"]; ok {
		patch.LabelsSet = true
		if !isJSONNull(v) {
			var ids []uuid.UUID
			if err := json.Unmarshal(v, &ids); err != nil {
				badField("label_ids")
				return decodedTaskPatch{}, false
			}
			patch.LabelIDs = ids
		}
	}
	if v, ok := raw["project_number"]; ok {
		decoded.ProjectNumberSet = true
		if !isJSONNull(v) {
			var number int64
			if err := json.Unmarshal(v, &number); err != nil || number <= 0 {
				badField("project_number")
				return decodedTaskPatch{}, false
			}
			decoded.ProjectNumber = &number
		}
	}
	if v, ok := raw["milestone_id"]; ok {
		decoded.MilestoneSet = true
		if !isJSONNull(v) {
			var id uuid.UUID
			if err := json.Unmarshal(v, &id); err != nil {
				badField("milestone_id")
				return decodedTaskPatch{}, false
			}
			decoded.MilestoneID = &id
		}
	}
	decoded.Patch = patch
	return decoded, true
}

func isJSONNull(raw json.RawMessage) bool {
	return string(raw) == "null"
}

func (h *taskHandler) update(w http.ResponseWriter, r *http.Request) {
	number, ok := parseTaskNumber(w, r)
	if !ok {
		return
	}
	decoded, ok := decodeTaskPatch(w, r)
	if !ok {
		return
	}
	patch := decoded.Patch
	if decoded.ProjectNumberSet {
		projectID, milestoneID, err := h.projects.ResolveTaskAssociation(r.Context(), decoded.ProjectNumber, decoded.MilestoneID)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		patch.ProjectSet = true
		patch.ProjectID = projectID
		if decoded.MilestoneSet {
			patch.MilestoneSet = true
			patch.MilestoneID = milestoneID
		}
	} else if decoded.MilestoneSet {
		patch.MilestoneSet = true
		if decoded.MilestoneID != nil {
			current, err := h.tasks.GetByNumber(r.Context(), number)
			if err != nil {
				WriteError(w, r, err)
				return
			}
			if current.Project == nil {
				WriteError(w, r, fmt.Errorf("%w: a milestone requires a project", domain.ErrInvalidInput))
				return
			}
			projectNumber := current.Project.Number
			_, milestoneID, err := h.projects.ResolveTaskAssociation(r.Context(), &projectNumber, decoded.MilestoneID)
			if err != nil {
				WriteError(w, r, err)
				return
			}
			patch.MilestoneID = milestoneID
		}
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	out, err := h.tasks.UpdateWithOperation(r.Context(), number, patch, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newTaskView(out))
}

func (h *taskHandler) archive(w http.ResponseWriter, r *http.Request) { h.setArchived(w, r, true) }
func (h *taskHandler) restore(w http.ResponseWriter, r *http.Request) { h.setArchived(w, r, false) }

func (h *taskHandler) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	number, ok := parseTaskNumber(w, r)
	if !ok {
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	out, err := h.tasks.SetArchivedWithOperation(r.Context(), number, archived, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newTaskView(out))
}

func (h *taskHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.TaskListFilter{
		Search:   q.Get("q"),
		Archived: q.Get("archived"),
		Sort:     q.Get("sort"),
		Order:    q.Get("order"),
		Cursor:   q.Get("cursor"),
	}
	for _, s := range q["status"] {
		st := domain.TaskStatus(s)
		if !st.Valid() {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "status is not a known value: " + s})
			return
		}
		f.Statuses = append(f.Statuses, st)
	}
	for _, p := range q["priority"] {
		pr := domain.TaskPriority(p)
		if !pr.Valid() {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "priority is not a known value: " + p})
			return
		}
		f.Priorities = append(f.Priorities, pr)
	}
	for _, l := range q["label"] {
		id, err := uuid.Parse(l)
		if err != nil {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "label is not a UUID: " + l})
			return
		}
		f.LabelIDs = append(f.LabelIDs, id)
	}
	if v := q.Get("assignee"); v != "" {
		if v == "none" {
			f.Unassigned = true
		} else {
			id, err := uuid.Parse(v)
			if err != nil {
				WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "assignee is not a UUID or \"none\""})
				return
			}
			f.AssigneeID = &id
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "limit must be a positive integer"})
			return
		}
		f.Limit = n
	}

	res, err := h.tasks.List(r.Context(), f)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	items := make([]taskView, len(res.Items))
	for i, it := range res.Items {
		items[i] = newTaskView(it)
	}
	WriteJSON(w, http.StatusOK, taskListResponse{Items: items, NextCursor: res.NextCursor, HasMore: res.HasMore})
}
