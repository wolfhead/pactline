package domain

import "fmt"

// TaskPhase describes how far a Task has progressed through delivery. It does
// not describe whether somebody currently owns the work in an active phase.
type TaskPhase string

const (
	TaskPhaseBacklog    TaskPhase = "backlog"
	TaskPhaseReady      TaskPhase = "ready"
	TaskPhaseInProgress TaskPhase = "in_progress"
	TaskPhaseInReview   TaskPhase = "in_review"
	TaskPhaseDone       TaskPhase = "done"
	TaskPhaseCancelled  TaskPhase = "cancelled"
)

func (p TaskPhase) Valid() bool {
	switch p {
	case TaskPhaseBacklog,
		TaskPhaseReady,
		TaskPhaseInProgress,
		TaskPhaseInReview,
		TaskPhaseDone,
		TaskPhaseCancelled:
		return true
	default:
		return false
	}
}

func (p TaskPhase) Active() bool {
	return p == TaskPhaseInProgress || p == TaskPhaseInReview
}

func (p TaskPhase) Terminal() bool {
	return p == TaskPhaseDone || p == TaskPhaseCancelled
}

// TaskActivityState describes ownership or a concrete blocker inside an
// active Task phase. Non-active phases deliberately have an empty activity.
type TaskActivityState string

const (
	TaskActivityAvailable       TaskActivityState = "available"
	TaskActivityWorking         TaskActivityState = "working"
	TaskActivityNeedsResolution TaskActivityState = "needs_resolution"
)

func (s TaskActivityState) Valid() bool {
	switch s {
	case TaskActivityAvailable, TaskActivityWorking, TaskActivityNeedsResolution:
		return true
	default:
		return false
	}
}

type TaskClaimStage string

const (
	TaskClaimStageExecution TaskClaimStage = "execution"
	TaskClaimStageReview    TaskClaimStage = "review"
)

func (s TaskClaimStage) Valid() bool {
	return s == TaskClaimStageExecution || s == TaskClaimStageReview
}

// TaskLifecycle is the consistency boundary for Task phase, activity, and
// acceptance-review cycle. Persistence may embed these fields in Task, but all
// business transitions go through this value rather than direct assignment.
type TaskLifecycle struct {
	Phase       TaskPhase
	Activity    TaskActivityState
	ReviewCycle int64
}

func NewTaskLifecycle() TaskLifecycle {
	return TaskLifecycle{Phase: TaskPhaseBacklog}
}

func (s TaskLifecycle) Validate() error {
	if !s.Phase.Valid() {
		return fmt.Errorf("%w: invalid Task phase %q", ErrInvalidInput, s.Phase)
	}
	if s.ReviewCycle < 0 {
		return fmt.Errorf("%w: Task review cycle cannot be negative", ErrInvalidInput)
	}
	if s.Phase.Active() {
		if !s.Activity.Valid() {
			return fmt.Errorf(
				"%w: active Task phase %q requires an activity state",
				ErrInvalidInput,
				s.Phase,
			)
		}
		return nil
	}
	if s.Activity != "" {
		return fmt.Errorf(
			"%w: Task phase %q cannot carry activity %q",
			ErrInvalidInput,
			s.Phase,
			s.Activity,
		)
	}
	return nil
}

func (s TaskLifecycle) Claimable() bool {
	return s.Phase == TaskPhaseReady ||
		(s.Phase.Active() && s.Activity == TaskActivityAvailable)
}

func (s *TaskLifecycle) MarkReady(archived, hasUnfinishedDependencies bool) error {
	if s.Phase != TaskPhaseBacklog || s.Activity != "" {
		return invalidTaskTransition("mark ready", *s)
	}
	if archived {
		return fmt.Errorf("%w: an archived Task cannot become ready", ErrConflict)
	}
	if hasUnfinishedDependencies {
		return fmt.Errorf("%w: a Task with unfinished dependencies cannot become ready", ErrConflict)
	}
	s.Phase = TaskPhaseReady
	return nil
}

func (s *TaskLifecycle) WithdrawReadiness() error {
	if s.Phase != TaskPhaseReady || s.Activity != "" {
		return invalidTaskTransition("withdraw readiness", *s)
	}
	s.Phase = TaskPhaseBacklog
	return nil
}

func (s *TaskLifecycle) Claim(archived bool) (TaskClaimStage, error) {
	if archived {
		return "", fmt.Errorf("%w: an archived Task cannot be claimed", ErrConflict)
	}
	switch {
	case s.Phase.Active() && s.Activity == TaskActivityWorking:
		return "", ErrActiveClaim
	case s.Phase == TaskPhaseReady && s.Activity == "":
		s.Phase = TaskPhaseInProgress
		s.Activity = TaskActivityWorking
		return TaskClaimStageExecution, nil
	case s.Phase == TaskPhaseInProgress && s.Activity == TaskActivityAvailable:
		s.Activity = TaskActivityWorking
		return TaskClaimStageExecution, nil
	case s.Phase == TaskPhaseInReview && s.Activity == TaskActivityAvailable:
		s.Activity = TaskActivityWorking
		return TaskClaimStageReview, nil
	default:
		return "", invalidTaskTransition("claim", *s)
	}
}

func (s *TaskLifecycle) Release(stage TaskClaimStage) error {
	if err := s.requireWorkingStage("release", stage); err != nil {
		return err
	}
	s.Activity = TaskActivityAvailable
	return nil
}

func (s *TaskLifecycle) RequestResolution(stage TaskClaimStage) error {
	if err := s.requireWorkingStage("request resolution", stage); err != nil {
		return err
	}
	s.Activity = TaskActivityNeedsResolution
	return nil
}

func (s *TaskLifecycle) ResolveIssue() error {
	if !s.Phase.Active() || s.Activity != TaskActivityNeedsResolution {
		return invalidTaskTransition("resolve issue", *s)
	}
	s.Activity = TaskActivityAvailable
	return nil
}

func (s *TaskLifecycle) CompleteExecution() error {
	if err := s.requireWorkingStage("complete execution", TaskClaimStageExecution); err != nil {
		return err
	}
	s.ReviewCycle++
	s.Phase = TaskPhaseInReview
	s.Activity = TaskActivityAvailable
	return nil
}

func (s *TaskLifecycle) RequestChanges() error {
	if err := s.requireWorkingStage("request changes", TaskClaimStageReview); err != nil {
		return err
	}
	s.Phase = TaskPhaseInProgress
	s.Activity = TaskActivityAvailable
	return nil
}

func (s *TaskLifecycle) Accept(readiness TaskCompletionReadiness) error {
	if err := s.requireWorkingStage("accept", TaskClaimStageReview); err != nil {
		return err
	}
	if err := (Task{}).ValidateCompletion(readiness); err != nil {
		return err
	}
	s.Phase = TaskPhaseDone
	s.Activity = ""
	return nil
}

func (s *TaskLifecycle) Cancel() error {
	if s.Phase.Terminal() {
		return invalidTaskTransition("cancel", *s)
	}
	if err := s.Validate(); err != nil {
		return err
	}
	s.Phase = TaskPhaseCancelled
	s.Activity = ""
	return nil
}

func (s TaskLifecycle) requireWorkingStage(action string, stage TaskClaimStage) error {
	if !stage.Valid() || s.Activity != TaskActivityWorking {
		return invalidTaskTransition(action, s)
	}
	expected := TaskClaimStageExecution
	if s.Phase == TaskPhaseInReview {
		expected = TaskClaimStageReview
	} else if s.Phase != TaskPhaseInProgress {
		return invalidTaskTransition(action, s)
	}
	if stage != expected {
		return fmt.Errorf(
			"%w: %s Claim cannot act in Task phase %q",
			ErrConflict,
			stage,
			s.Phase,
		)
	}
	return nil
}

func invalidTaskTransition(action string, state TaskLifecycle) error {
	return fmt.Errorf(
		"%w: cannot %s from Task state %s.%s",
		ErrInvalidTransition,
		action,
		state.Phase,
		state.Activity,
	)
}
