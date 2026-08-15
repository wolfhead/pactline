package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
)

func TestTaskValidateCompletionIncludesChildrenAndDependencies(t *testing.T) {
	task := domain.Task{}

	for _, test := range []struct {
		name      string
		readiness domain.TaskCompletionReadiness
	}{
		{
			name: "unsatisfied acceptance",
			readiness: domain.TaskCompletionReadiness{
				ActiveCriteria: 1, UnsatisfiedCriteria: 1,
			},
		},
		{
			name: "unfinished child",
			readiness: domain.TaskCompletionReadiness{
				UnfinishedChildren: 1,
			},
		},
		{
			name: "unfinished dependency",
			readiness: domain.TaskCompletionReadiness{
				UnfinishedDependencies: 1,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := task.ValidateCompletion(test.readiness); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("ValidateCompletion() error = %v, want ErrConflict", err)
			}
		})
	}

	if err := task.ValidateCompletion(domain.TaskCompletionReadiness{}); err != nil {
		t.Fatalf("ValidateCompletion() error = %v, want nil", err)
	}
}

func TestTaskReadinessAcceptanceGateIgnoresCompletionRelationships(t *testing.T) {
	readiness := domain.TaskCompletionReadiness{
		ActiveCriteria:         2,
		UnfinishedChildren:     1,
		UnfinishedDependencies: 1,
	}
	if err := readiness.ValidateAcceptance(); err != nil {
		t.Fatalf("ValidateAcceptance() error = %v, want nil", err)
	}

	readiness.UnsatisfiedCriteria = 1
	if err := readiness.ValidateAcceptance(); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("ValidateAcceptance() error = %v, want ErrConflict", err)
	}
}

func TestValidateSchedule(t *testing.T) {
	start := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	before := start.AddDate(0, 0, -1)
	after := start.AddDate(0, 0, 1)

	for _, test := range []struct {
		name  string
		start *time.Time
		due   *time.Time
		valid bool
	}{
		{name: "unscheduled", valid: true},
		{name: "due only", due: &before, valid: true},
		{name: "start only", start: &start, valid: true},
		{name: "same day", start: &start, due: &start, valid: true},
		{name: "ordered range", start: &start, due: &after, valid: true},
		{name: "reversed range", start: &start, due: &before},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := domain.ValidateSchedule(test.start, test.due)
			if test.valid && err != nil {
				t.Fatalf("ValidateSchedule() error = %v, want nil", err)
			}
			if !test.valid && !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("ValidateSchedule() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestTaskPatchIsEmptyDistinguishesScheduleClearFromAbsence(t *testing.T) {
	scheduled := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		name  string
		patch domain.TaskPatch
		empty bool
	}{
		{
			name:  "absent schedule field leaves task unchanged",
			patch: domain.TaskPatch{},
			empty: true,
		},
		{
			name: "explicit start date clear is a mutation",
			patch: domain.TaskPatch{
				StartDateSet: true,
			},
		},
		{
			name: "explicit start date set is a mutation",
			patch: domain.TaskPatch{
				StartDateSet: true,
				StartDate:    &scheduled,
			},
		},
		{
			name: "explicit due date clear is a mutation",
			patch: domain.TaskPatch{
				DueDateSet: true,
			},
		},
		{
			name: "explicit due date set is a mutation",
			patch: domain.TaskPatch{
				DueDateSet: true,
				DueDate:    &scheduled,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.patch.IsEmpty(); got != test.empty {
				t.Fatalf("IsEmpty() = %v, want %v", got, test.empty)
			}
		})
	}
}
