package usecase

import (
	"time"

	"admin-bot/internal/domain"
)

// AccessUseCase implements the access-request workflow on top of an
// AccessStore.
type AccessUseCase struct {
	store domain.AccessStore
	now   func() time.Time // injectable clock, defaults to time.Now
}

// NewAccessUseCase wires the use case to a store.
func NewAccessUseCase(store domain.AccessStore) *AccessUseCase {
	return &AccessUseCase{store: store, now: time.Now}
}

// RequestAccess creates a pending request for a first-time user and returns it.
// If a request already exists (in any state) it is returned unchanged — the
// operation is idempotent, so a user spamming /start never produces duplicates
// nor resets a prior decision.
func (uc *AccessUseCase) RequestAccess(userID int64, username, firstName string) (domain.AccessRequest, error) {
	if existing, ok := uc.store.Get(userID); ok {
		return existing, nil
	}
	req := domain.AccessRequest{
		UserID:      userID,
		Username:    username,
		FirstName:   firstName,
		Status:      domain.AccessPending,
		RequestedAt: uc.now(),
	}
	if err := uc.store.Save(req); err != nil {
		return domain.AccessRequest{}, err
	}
	return req, nil
}

// Approve marks a user approved, preserving their existing profile fields. A
// user with no prior request is created directly in the approved state (used
// for pre-seeding admins).
func (uc *AccessUseCase) Approve(userID int64) (domain.AccessRequest, error) {
	return uc.decide(userID, domain.AccessApproved)
}

// Reject marks a user rejected.
func (uc *AccessUseCase) Reject(userID int64) (domain.AccessRequest, error) {
	return uc.decide(userID, domain.AccessRejected)
}

func (uc *AccessUseCase) decide(userID int64, status domain.AccessStatus) (domain.AccessRequest, error) {
	req, ok := uc.store.Get(userID)
	if !ok {
		req = domain.AccessRequest{UserID: userID, RequestedAt: uc.now()}
	}
	req.Status = status
	req.DecidedAt = uc.now()
	if err := uc.store.Save(req); err != nil {
		return domain.AccessRequest{}, err
	}
	return req, nil
}

// IsApproved reports whether the user has an approved request.
func (uc *AccessUseCase) IsApproved(userID int64) bool {
	req, ok := uc.store.Get(userID)
	return ok && req.Status == domain.AccessApproved
}

// Get returns the current request for a user and whether one exists. Used by
// the delivery layer to branch on the exact status (pending/approved/rejected)
// without creating a request as a side effect.
func (uc *AccessUseCase) Get(userID int64) (domain.AccessRequest, bool) {
	return uc.store.Get(userID)
}
