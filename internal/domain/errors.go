package domain

import "errors"

var (
	// ErrNotFound is returned when a requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalidTransition means the requested status is unreachable from the
	// current one.
	ErrInvalidTransition = errors.New("invalid status transition")
	// ErrRetrospectiveRequired means an abandoned bounty carries no conclusion.
	ErrRetrospectiveRequired = errors.New("retrospective is required when abandoning")
	// ErrNotClaimable means the bounty is not open.
	ErrNotClaimable = errors.New("bounty is not open for claiming")
	// ErrNotDirectedToYou means a directed bounty targets someone else.
	ErrNotDirectedToYou = errors.New("bounty is directed to another user")
	// ErrForbidden means the actor lacks the required role.
	ErrForbidden = errors.New("forbidden")
	// ErrNotYourCredit means the actor is not the nominee of this credit.
	ErrNotYourCredit = errors.New("credit belongs to another user")
	// ErrCreditNotPending means the credit was already confirmed or declined.
	ErrCreditNotPending = errors.New("credit is not pending")
	// ErrEvidenceRequired means a REVIEW credit carries no review record.
	ErrEvidenceRequired = errors.New("evidence is required for REVIEW credit")
	// ErrInvalidCreditRole means the nominated role is not one of the six
	// defined parts.
	ErrInvalidCreditRole = errors.New("invalid credit role")
)
