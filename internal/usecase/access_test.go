package usecase

import (
	"testing"

	"admin-bot/internal/domain"
)

// fakeAccessStore is an in-memory AccessStore for use-case tests.
type fakeAccessStore struct {
	reqs  map[int64]domain.AccessRequest
	saves int
}

func newFakeAccessStore() *fakeAccessStore {
	return &fakeAccessStore{reqs: make(map[int64]domain.AccessRequest)}
}

func (f *fakeAccessStore) Get(userID int64) (domain.AccessRequest, bool) {
	r, ok := f.reqs[userID]
	return r, ok
}

func (f *fakeAccessStore) Save(req domain.AccessRequest) error {
	f.reqs[req.UserID] = req
	f.saves++
	return nil
}

func (f *fakeAccessStore) ListPending() ([]domain.AccessRequest, error) {
	var out []domain.AccessRequest
	for _, r := range f.reqs {
		if r.Status == domain.AccessPending {
			out = append(out, r)
		}
	}
	return out, nil
}

func TestRequestAccessIdempotent(t *testing.T) {
	store := newFakeAccessStore()
	uc := NewAccessUseCase(store)

	first, err := uc.RequestAccess(1, "alice", "Alice")
	if err != nil {
		t.Fatalf("RequestAccess: %v", err)
	}
	if first.Status != domain.AccessPending {
		t.Fatalf("expected pending, got %s", first.Status)
	}
	if store.saves != 1 {
		t.Fatalf("expected 1 save, got %d", store.saves)
	}

	// Repeated calls must not create duplicates nor extra saves.
	again, err := uc.RequestAccess(1, "alice-renamed", "Alice2")
	if err != nil {
		t.Fatalf("RequestAccess again: %v", err)
	}
	if store.saves != 1 {
		t.Fatalf("repeat RequestAccess wrote again: %d saves", store.saves)
	}
	if again.Username != "alice" {
		t.Fatalf("existing record should be returned unchanged, got %q", again.Username)
	}
}

func TestRequestAccessDoesNotResetDecision(t *testing.T) {
	store := newFakeAccessStore()
	uc := NewAccessUseCase(store)

	if _, err := uc.RequestAccess(1, "alice", "Alice"); err != nil {
		t.Fatalf("RequestAccess: %v", err)
	}
	if _, err := uc.Reject(1); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	// A rejected user calling /start again stays rejected.
	got, err := uc.RequestAccess(1, "alice", "Alice")
	if err != nil {
		t.Fatalf("RequestAccess after reject: %v", err)
	}
	if got.Status != domain.AccessRejected {
		t.Fatalf("expected rejected preserved, got %s", got.Status)
	}
}

func TestApproveRejectTransitions(t *testing.T) {
	store := newFakeAccessStore()
	uc := NewAccessUseCase(store)

	if _, err := uc.RequestAccess(1, "alice", "Alice"); err != nil {
		t.Fatalf("RequestAccess: %v", err)
	}
	if uc.IsApproved(1) {
		t.Fatal("pending user must not be approved")
	}

	approved, err := uc.Approve(1)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Status != domain.AccessApproved || approved.DecidedAt.IsZero() {
		t.Fatalf("unexpected approved record: %+v", approved)
	}
	if approved.Username != "alice" {
		t.Fatalf("Approve should preserve profile, got %q", approved.Username)
	}
	if !uc.IsApproved(1) {
		t.Fatal("user should be approved")
	}

	if _, err := uc.Reject(1); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if uc.IsApproved(1) {
		t.Fatal("rejected user must not be approved")
	}
}

// TestApproveUnknownUser covers pre-seeding an admin who never sent /start.
func TestApproveUnknownUser(t *testing.T) {
	store := newFakeAccessStore()
	uc := NewAccessUseCase(store)

	if _, err := uc.Approve(999); err != nil {
		t.Fatalf("Approve unknown: %v", err)
	}
	if !uc.IsApproved(999) {
		t.Fatal("pre-seeded admin should be approved")
	}
}

func TestIsApprovedUnknown(t *testing.T) {
	uc := NewAccessUseCase(newFakeAccessStore())
	if uc.IsApproved(123) {
		t.Fatal("unknown user must not be approved")
	}
}
