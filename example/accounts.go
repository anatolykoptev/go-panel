package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
)

// memAccounts is the demo's in-memory AccountStore: enough for the multi-user
// sign-in, the Settings pages and a real password change. A deployment uses
// auth.PgxAccountStore.
type memAccounts struct {
	mu   sync.Mutex
	byID map[string]*auth.Account
}

func newMemAccounts() *memAccounts { return &memAccounts{byID: map[string]*auth.Account{}} }

func (s *memAccounts) find(email string) *auth.Account {
	for _, a := range s.byID {
		if a.Email == email {
			return a
		}
	}
	return nil
}

func (s *memAccounts) GetByEmail(_ context.Context, email string) (*auth.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.find(email)
	if a == nil || !a.Active {
		return nil, auth.ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (s *memAccounts) GetByID(_ context.Context, id string) (*auth.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byID[id]
	if !ok {
		return nil, auth.ErrAccountNotFound
	}
	cp := *a
	cp.PasswordHash = ""
	return &cp, nil
}

func (s *memAccounts) UpdateLastLogin(context.Context, string) error { return nil }

func (s *memAccounts) UpdatePasswordHash(_ context.Context, id, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	now := time.Now()
	a.PasswordHash, a.PasswordChangedAt = hash, &now
	return nil
}

// stamp is the auth.EpochStore contract: stamp the credential epoch (and the
// password hash, when given) only if the caller's session is still current,
// atomically under the lock, and return the stamp.
func (s *memAccounts) stamp(id, hash string, callerIatMs int64) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byID[id]
	if !ok || (a.PasswordChangedAt != nil && a.PasswordChangedAt.After(time.UnixMilli(callerIatMs))) {
		return time.Time{}, auth.ErrSessionStale
	}
	now := time.Now()
	if hash != "" {
		a.PasswordHash = hash
	}
	a.PasswordChangedAt = &now
	return now, nil
}

// UpdatePasswordHashIfCurrent implements auth.EpochStore.
func (s *memAccounts) UpdatePasswordHashIfCurrent(_ context.Context, id, hash string, callerIatMs int64) (time.Time, error) {
	return s.stamp(id, hash, callerIatMs)
}

// RevokeSessionsIfCurrent implements auth.EpochStore.
func (s *memAccounts) RevokeSessionsIfCurrent(_ context.Context, id string, callerIatMs int64) (time.Time, error) {
	return s.stamp(id, "", callerIatMs)
}

func (s *memAccounts) CreateAccount(_ context.Context, email, name, hash, role string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.find(email); a != nil {
		return a.ID, false, nil
	}
	id, _, _ := strings.Cut(email, "@")
	s.byID[id] = &auth.Account{ID: id, Email: email, Name: name, PasswordHash: hash, Role: role, Active: true}
	return id, true, nil
}
