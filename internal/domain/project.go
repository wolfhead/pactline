package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ProjectStatus string

const (
	ProjectStatusPlanned   ProjectStatus = "planned"
	ProjectStatusActive    ProjectStatus = "active"
	ProjectStatusPaused    ProjectStatus = "paused"
	ProjectStatusCompleted ProjectStatus = "completed"
	ProjectStatusCancelled ProjectStatus = "cancelled"
)

func (s ProjectStatus) Valid() bool {
	switch s {
	case ProjectStatusPlanned, ProjectStatusActive, ProjectStatusPaused, ProjectStatusCompleted, ProjectStatusCancelled:
		return true
	}
	return false
}

type Project struct {
	ID          uuid.UUID
	Number      int64
	Name        string
	Outcome     string
	Description string
	OwnerID     uuid.UUID
	CreatorID   uuid.UUID
	Status      ProjectStatus
	TargetDate  *time.Time
	CompletedAt *time.Time
	CancelledAt *time.Time
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProjectActivity struct {
	ID          uuid.UUID
	ProjectID   uuid.UUID
	MilestoneID *uuid.UUID
	ActorID     uuid.UUID
	Action      string
	Reason      *string
	OldValue    *string
	NewValue    *string
	CreatedAt   time.Time
}

func (p Project) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: project name is required", ErrInvalidInput)
	}
	if strings.TrimSpace(p.Outcome) == "" {
		return fmt.Errorf("%w: project outcome is required", ErrInvalidInput)
	}
	if p.OwnerID == uuid.Nil {
		return fmt.Errorf("%w: project owner is required", ErrInvalidInput)
	}
	if p.CreatorID == uuid.Nil {
		return fmt.Errorf("%w: project creator is required", ErrInvalidInput)
	}
	if !p.Status.Valid() {
		return fmt.Errorf("%w: invalid project status %q", ErrInvalidInput, p.Status)
	}
	return nil
}

type ProjectReadiness struct {
	ActiveCriteria      int
	UnsatisfiedCriteria int
	OpenMilestones      int
	UnfinishedTasks     int
}

func (p *Project) Activate(readiness ProjectReadiness) error {
	if p.Status != ProjectStatusPlanned && p.Status != ProjectStatusPaused {
		return fmt.Errorf("%w: project cannot activate from %s", ErrConflict, p.Status)
	}
	if readiness.ActiveCriteria == 0 {
		return fmt.Errorf("%w: project requires an active acceptance criterion", ErrConflict)
	}
	p.Status = ProjectStatusActive
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (p *Project) Pause() error {
	if p.Status != ProjectStatusActive {
		return fmt.Errorf("%w: project cannot pause from %s", ErrConflict, p.Status)
	}
	p.Status = ProjectStatusPaused
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (p *Project) Complete(readiness ProjectReadiness) error {
	if p.Status != ProjectStatusActive && p.Status != ProjectStatusPaused {
		return fmt.Errorf("%w: project cannot complete from %s", ErrConflict, p.Status)
	}
	if readiness.ActiveCriteria == 0 || readiness.UnsatisfiedCriteria > 0 {
		return fmt.Errorf("%w: project acceptance is not satisfied", ErrConflict)
	}
	if readiness.OpenMilestones > 0 {
		return fmt.Errorf("%w: project has open milestones", ErrConflict)
	}
	if readiness.UnfinishedTasks > 0 {
		return fmt.Errorf("%w: project has unfinished tasks", ErrConflict)
	}
	now := time.Now().UTC()
	p.Status = ProjectStatusCompleted
	p.CompletedAt = &now
	p.CancelledAt = nil
	p.UpdatedAt = now
	return nil
}

func (p *Project) Cancel(readiness ProjectReadiness) error {
	if p.Status != ProjectStatusPlanned && p.Status != ProjectStatusActive && p.Status != ProjectStatusPaused {
		return fmt.Errorf("%w: project cannot cancel from %s", ErrConflict, p.Status)
	}
	if readiness.OpenMilestones > 0 {
		return fmt.Errorf("%w: project has open milestones", ErrConflict)
	}
	if readiness.UnfinishedTasks > 0 {
		return fmt.Errorf("%w: project has unfinished tasks", ErrConflict)
	}
	now := time.Now().UTC()
	p.Status = ProjectStatusCancelled
	p.CancelledAt = &now
	p.CompletedAt = nil
	p.UpdatedAt = now
	return nil
}

func (p *Project) Reopen(actor Actor, reason string) error {
	if !actor.IsHuman() {
		return fmt.Errorf("%w: only a human user may reopen a project", ErrForbidden)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: reopen reason is required", ErrInvalidInput)
	}
	if p.Status != ProjectStatusCompleted && p.Status != ProjectStatusCancelled {
		return fmt.Errorf("%w: project cannot reopen from %s", ErrConflict, p.Status)
	}
	p.Status = ProjectStatusActive
	p.CompletedAt = nil
	p.CancelledAt = nil
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (p *Project) Archive() error {
	if p.Status != ProjectStatusCompleted && p.Status != ProjectStatusCancelled {
		return fmt.Errorf("%w: only a concluded project may be archived", ErrConflict)
	}
	now := time.Now().UTC()
	p.ArchivedAt = &now
	p.UpdatedAt = now
	return nil
}

func (p *Project) Restore() {
	p.ArchivedAt = nil
	p.UpdatedAt = time.Now().UTC()
}

type MilestoneStatus string

const (
	MilestoneStatusOpen      MilestoneStatus = "open"
	MilestoneStatusCompleted MilestoneStatus = "completed"
	MilestoneStatusCancelled MilestoneStatus = "cancelled"
)

func (s MilestoneStatus) Valid() bool {
	switch s {
	case MilestoneStatusOpen, MilestoneStatusCompleted, MilestoneStatusCancelled:
		return true
	}
	return false
}

type Milestone struct {
	ID          uuid.UUID
	ProjectID   uuid.UUID
	Name        string
	Outcome     string
	Description string
	Status      MilestoneStatus
	TargetDate  *time.Time
	Position    int
	CompletedAt *time.Time
	CancelledAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (m Milestone) Validate() error {
	if m.ProjectID == uuid.Nil || strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Outcome) == "" {
		return fmt.Errorf("%w: milestone project, name, and outcome are required", ErrInvalidInput)
	}
	if !m.Status.Valid() || m.Position < 0 {
		return fmt.Errorf("%w: invalid milestone status or position", ErrInvalidInput)
	}
	return nil
}

type MilestoneReadiness struct {
	ActiveCriteria      int
	UnsatisfiedCriteria int
	UnfinishedTasks     int
}

func (m *Milestone) Complete(readiness MilestoneReadiness) error {
	if m.Status != MilestoneStatusOpen {
		return fmt.Errorf("%w: milestone cannot complete from %s", ErrConflict, m.Status)
	}
	if readiness.ActiveCriteria == 0 || readiness.UnsatisfiedCriteria > 0 {
		return fmt.Errorf("%w: milestone acceptance is not satisfied", ErrConflict)
	}
	if readiness.UnfinishedTasks > 0 {
		return fmt.Errorf("%w: milestone has unfinished tasks", ErrConflict)
	}
	now := time.Now().UTC()
	m.Status = MilestoneStatusCompleted
	m.CompletedAt = &now
	m.CancelledAt = nil
	m.UpdatedAt = now
	return nil
}

func (m *Milestone) Cancel(readiness MilestoneReadiness) error {
	if m.Status != MilestoneStatusOpen {
		return fmt.Errorf("%w: milestone cannot cancel from %s", ErrConflict, m.Status)
	}
	if readiness.UnfinishedTasks > 0 {
		return fmt.Errorf("%w: milestone has unfinished tasks", ErrConflict)
	}
	now := time.Now().UTC()
	m.Status = MilestoneStatusCancelled
	m.CancelledAt = &now
	m.CompletedAt = nil
	m.UpdatedAt = now
	return nil
}

func (m *Milestone) Reopen(actor Actor, reason string) error {
	if !actor.IsHuman() {
		return fmt.Errorf("%w: only a human user may reopen a milestone", ErrForbidden)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: reopen reason is required", ErrInvalidInput)
	}
	if m.Status != MilestoneStatusCompleted && m.Status != MilestoneStatusCancelled {
		return fmt.Errorf("%w: milestone cannot reopen from %s", ErrConflict, m.Status)
	}
	m.Status = MilestoneStatusOpen
	m.CompletedAt = nil
	m.CancelledAt = nil
	m.UpdatedAt = time.Now().UTC()
	return nil
}
