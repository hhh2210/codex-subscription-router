package mux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/b-nnett/codex-subscription-router/internal/state"
)

func (m *Multiplexer) ImportAccount(ctx context.Context, label string, authJSON json.RawMessage) (AccountSnapshot, error) {
	m.provisioningMu.Lock()
	defer m.provisioningMu.Unlock()
	if len(m.pendingLogins) != 0 {
		return AccountSnapshot{}, errors.New("finish or cancel the pending ChatGPT login before importing credentials")
	}
	account, err := m.store.AddAccountWithAuth(label, authJSON)
	if err != nil {
		return AccountSnapshot{}, err
	}
	if _, err := m.startChild(ctx, account); err != nil {
		return AccountSnapshot{}, m.rollbackImportedAccount(account, fmt.Errorf("start imported account: %w", err))
	}
	snapshot, err := m.accountSnapshotWithProfile(ctx, account.ID, false)
	if err != nil {
		return AccountSnapshot{}, m.rollbackImportedAccount(account, fmt.Errorf("verify imported account: %w", err))
	}
	if !snapshot.Connected || snapshot.AuthType != "chatgpt" {
		return AccountSnapshot{}, m.rollbackImportedAccount(account, errors.New("imported auth.json did not produce a ChatGPT login"))
	}
	m.publish(Event{Type: "account-updated", AccountID: account.ID, Data: snapshot})
	return snapshot, nil
}

func (m *Multiplexer) rollbackImportedAccount(account state.Account, cause error) error {
	m.childrenMu.Lock()
	child := m.children[account.ID]
	delete(m.children, account.ID)
	m.childrenMu.Unlock()

	var cleanupErrors []error
	if child != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := child.Stop(stopCtx); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("stop child: %w", err))
		}
		cancel()
	}
	if err := m.store.DiscardImportedAccount(account.ID); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	if len(cleanupErrors) > 0 {
		return fmt.Errorf("%w; rollback failed: %v", cause, errors.Join(cleanupErrors...))
	}
	return cause
}
