package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type taskHandler struct {
	tasks *store.TaskStore
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
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Status      string      `json:"status"`
	Priority    string      `json:"priority"`
	AssigneeID  *uuid.UUID  `json:"assignee_id"`
	DueDate     *string     `json:"due_date"`
	LabelIDs    []uuid.UUID `json:"label_ids"`
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

	out, err := h.tasks.Create(r.Context(), domain.Task{
		Title:       req.Title,
		Description: req.Description,
		Status:      domain.TaskStatus(req.Status),
		Priority:    domain.TaskPriority(req.Priority),
		AssigneeID:  req.AssigneeID,
		CreatorID:   me.ID,
		DueDate:     dueDate,
	}, req.LabelIDs)
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
func decodeTaskPatch(w http.ResponseWriter, r *http.Request) (domain.TaskPatch, bool) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		slog.Warn("decode task patch body", "path", r.URL.Path, "error", err)
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid JSON body"})
		return domain.TaskPatch{}, false
	}

	var patch domain.TaskPatch
	badField := func(field string) {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: field + " is malformed"})
	}

	if v, ok := raw["title"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			badField("title")
			return domain.TaskPatch{}, false
		}
		patch.Title = &s
	}
	if v, ok := raw["description"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			badField("description")
			return domain.TaskPatch{}, false
		}
		patch.Description = &s
	}
	if v, ok := raw["status"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			badField("status")
			return domain.TaskPatch{}, false
		}
		st := domain.TaskStatus(s)
		patch.Status = &st
	}
	if v, ok := raw["priority"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			badField("priority")
			return domain.TaskPatch{}, false
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
				return domain.TaskPatch{}, false
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
				return domain.TaskPatch{}, false
			}
			d, ok := parseDueDate(w, s)
			if !ok {
				return domain.TaskPatch{}, false
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
				return domain.TaskPatch{}, false
			}
			patch.LabelIDs = ids
		}
	}
	return patch, true
}

func isJSONNull(raw json.RawMessage) bool {
	return string(raw) == "null"
}

func (h *taskHandler) update(w http.ResponseWriter, r *http.Request) {
	number, ok := parseTaskNumber(w, r)
	if !ok {
		return
	}
	patch, ok := decodeTaskPatch(w, r)
	if !ok {
		return
	}
	me := CurrentUser(r)
	out, err := h.tasks.Update(r.Context(), number, patch, me.ID)
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
	me := CurrentUser(r)
	out, err := h.tasks.SetArchived(r.Context(), number, archived, me.ID)
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
