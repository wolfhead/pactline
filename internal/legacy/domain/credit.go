package domain

import (
	"time"

	userdomain "github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
)

// CreditRole is the part someone played in a work. Credits are the mechanism's
// record of collaboration: naming a second person on a work costs nobody
// anything, which is what keeps collaboration positive-sum.
type CreditRole string

const (
	CreditRoleDefine    CreditRole = "DEFINE"
	CreditRoleLead      CreditRole = "LEAD"
	CreditRoleCoDeliver CreditRole = "CO_DELIVER"
	CreditRoleReview    CreditRole = "REVIEW"
	CreditRoleSupport   CreditRole = "SUPPORT"
	CreditRoleBaseline  CreditRole = "BASELINE"
)

// CreditStatus tracks nominee acknowledgement.
type CreditStatus string

const (
	CreditPending   CreditStatus = "PENDING"
	CreditConfirmed CreditStatus = "CONFIRMED"
	CreditDeclined  CreditStatus = "DECLINED"
)

// Credit attributes a role on a bounty to a user.
//
// NominatedBy is nil for system-generated credits (a delivery bounty inheriting
// its plan author). Those are still PENDING and still require the nominee to
// confirm: the system only ensures nobody is forgotten, it never claims a
// credit on someone's behalf.
type Credit struct {
	ID          uuid.UUID    `json:"id"`
	BountyID    uuid.UUID    `json:"bounty_id"`
	UserID      uuid.UUID    `json:"user_id"`
	Role        CreditRole   `json:"role"`
	NominatedBy *uuid.UUID   `json:"nominated_by,omitempty"`
	Evidence    string       `json:"evidence,omitempty"`
	Status      CreditStatus `json:"status"`
	ConfirmedAt *time.Time   `json:"confirmed_at,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
}

var validCreditRoles = map[CreditRole]bool{
	CreditRoleDefine:    true,
	CreditRoleLead:      true,
	CreditRoleCoDeliver: true,
	CreditRoleReview:    true,
	CreditRoleSupport:   true,
	CreditRoleBaseline:  true,
}

// ValidCreditRole reports whether the role is one of the six defined parts.
func ValidCreditRole(r CreditRole) bool { return validCreditRoles[r] }

// ValidateNomination checks role validity and the review-evidence rule.
//
// A REVIEW credit without evidence is rejected because otherwise the role
// degrades into mutual back-patting, which would make review credit worthless
// exactly where the mechanism needs it to carry weight.
func ValidateNomination(c Credit) error {
	if !ValidCreditRole(c.Role) {
		return ErrInvalidCreditRole
	}
	if c.Role == CreditRoleReview && c.Evidence == "" {
		return ErrEvidenceRequired
	}
	return nil
}

// CanRespond reports whether the user may confirm or decline this credit.
// Only the nominee may — including against the steward.
func CanRespond(u userdomain.User, c Credit) error {
	if c.UserID != u.ID {
		return ErrNotYourCredit
	}
	if c.Status != CreditPending {
		return ErrCreditNotPending
	}
	return nil
}
