package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID          uuid.UUID
	Number      int64
	Version     int64
	Name        string
	Description string
	CreatorID   uuid.UUID
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
	RequestID   *string
	AuthMethod  *AuthenticationMethod
	APITokenID  *uuid.UUID
	TokenName   *string
	AgentRunID  *uuid.UUID
	CreatedAt   time.Time
}

func (p Project) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: project name is required", ErrInvalidInput)
	}
	if p.CreatorID == uuid.Nil {
		return fmt.Errorf("%w: project creator is required", ErrInvalidInput)
	}
	return nil
}

type ProjectArchiveReadiness struct {
	OpenMilestones  int
	UnfinishedTasks int
}

func (p *Project) Archive(readiness ProjectArchiveReadiness) error {
	if readiness.OpenMilestones > 0 {
		return fmt.Errorf("%w: project has planned or active milestones", ErrConflict)
	}
	if readiness.UnfinishedTasks > 0 {
		return fmt.Errorf("%w: project has unfinished tasks", ErrConflict)
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
	MilestoneStatusPlanned   MilestoneStatus = "planned"
	MilestoneStatusActive    MilestoneStatus = "active"
	MilestoneStatusCompleted MilestoneStatus = "completed"
	MilestoneStatusCancelled MilestoneStatus = "cancelled"
)

func (s MilestoneStatus) Valid() bool {
	switch s {
	case MilestoneStatusPlanned, MilestoneStatusActive, MilestoneStatusCompleted, MilestoneStatusCancelled:
		return true
	}
	return false
}

type Milestone struct {
	ID          uuid.UUID
	ProjectID   uuid.UUID
	Version     int64
	Name        string
	Outcome     string
	Description string
	OwnerID     uuid.UUID
	Status      MilestoneStatus
	TargetDate  *time.Time
	Position    int
	CompletedAt *time.Time
	CancelledAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (m Milestone) Validate() error {
	if m.ProjectID == uuid.Nil || strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Outcome) == "" || m.OwnerID == uuid.Nil {
		return fmt.Errorf("%w: milestone project, name, outcome, and owner are required", ErrInvalidInput)
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

func (m *Milestone) Activate(readiness MilestoneReadiness) error {
	if m.Status != MilestoneStatusPlanned {
		return fmt.Errorf("%w: milestone cannot activate from %s", ErrConflict, m.Status)
	}
	if readiness.ActiveCriteria == 0 {
		return fmt.Errorf("%w: milestone requires an active acceptance criterion", ErrConflict)
	}
	m.Status = MilestoneStatusActive
	m.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *Milestone) Complete(readiness MilestoneReadiness) error {
	if m.Status != MilestoneStatusActive {
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
	if m.Status != MilestoneStatusPlanned && m.Status != MilestoneStatusActive {
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
	m.Status = MilestoneStatusActive
	m.CompletedAt = nil
	m.CancelledAt = nil
	m.UpdatedAt = time.Now().UTC()
	return nil
}
