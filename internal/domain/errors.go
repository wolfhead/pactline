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
)
