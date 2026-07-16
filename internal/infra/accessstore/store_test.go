package accessstore

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"admin-bot/internal/domain"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "access.json") // sub/ must be auto-created
	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, path
}

func TestSaveGet(t *testing.T) {
	s, _ := newTestStore(t)

	if _, ok := s.Get(42); ok {
		t.Fatal("expected no record for unknown user")
	}

	req := domain.AccessRequest{
		UserID:      42,
		Username:    "alice",
		FirstName:   "Alice",
		Status:      domain.AccessPending,
		RequestedAt: time.Unix(1000, 0),
	}
	if err := s.Save(req); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, ok := s.Get(42)
	if !ok {
		t.Fatal("expected record after Save")
	}
	if got.Username != "alice" || got.Status != domain.AccessPending {
		t.Fatalf("unexpected record: %+v", got)
	}

	// Overwrite updates in place.
	req.Status = domain.AccessApproved
	if err := s.Save(req); err != nil {
		t.Fatalf("Save overwrite: %v", err)
	}
	got, _ = s.Get(42)
	if got.Status != domain.AccessApproved {
		t.Fatalf("expected approved after overwrite, got %s", got.Status)
	}
}

func TestListPendingSorted(t *testing.T) {
	s, _ := newTestStore(t)

	must := func(r domain.AccessRequest) {
		if err := s.Save(r); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	must(domain.AccessRequest{UserID: 1, Status: domain.AccessPending, RequestedAt: time.Unix(300, 0)})
	must(domain.AccessRequest{UserID: 2, Status: domain.AccessApproved, RequestedAt: time.Unix(100, 0)})
	must(domain.AccessRequest{UserID: 3, Status: domain.AccessPending, RequestedAt: time.Unix(100, 0)})
	must(domain.AccessRequest{UserID: 4, Status: domain.AccessRejected, RequestedAt: time.Unix(50, 0)})

	pending, err := s.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
	// Oldest RequestedAt first: user 3 (t=100) before user 1 (t=300).
	if pending[0].UserID != 3 || pending[1].UserID != 1 {
		t.Fatalf("unexpected order: %d, %d", pending[0].UserID, pending[1].UserID)
	}
}

// TestPersistenceSurvivesReopen writes with one Store, opens a fresh Store on
// the same path, and asserts the data is intact.
func TestPersistenceSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.json")
	s1, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := domain.AccessRequest{
		UserID:      7,
		Username:    "bob",
		FirstName:   "Bob",
		Status:      domain.AccessApproved,
		RequestedAt: time.Unix(1234, 0).UTC(),
		DecidedAt:   time.Unix(5678, 0).UTC(),
	}
	if err := s1.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := New(path)
	if err != nil {
		t.Fatalf("reopen New: %v", err)
	}
	got, ok := s2.Get(7)
	if !ok {
		t.Fatal("record lost across reopen")
	}
	if got.Username != want.Username || got.Status != want.Status ||
		!got.RequestedAt.Equal(want.RequestedAt) || !got.DecidedAt.Equal(want.DecidedAt) {
		t.Fatalf("record changed across reopen:\n got %+v\nwant %+v", got, want)
	}
}

// TestConcurrentAccess exercises the RWMutex under -race.
func TestConcurrentAccess(t *testing.T) {
	s, _ := newTestStore(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Save(domain.AccessRequest{
				UserID:      int64(i),
				Status:      domain.AccessPending,
				RequestedAt: time.Unix(int64(i), 0),
			})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Get(int64(i))
			_, _ = s.ListPending()
		}()
	}
	wg.Wait()

	// A fresh store must be able to parse whatever the concurrent writers left.
	if _, err := New(s.path); err != nil {
		t.Fatalf("store unreadable after concurrent writes: %v", err)
	}
}
