package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const MaxImportedAuthBytes = 64 * 1024

var ErrDuplicateChatGPTAccount = errors.New("ChatGPT account is already configured")

type importedAuthDocument struct {
	AuthMode     string          `json:"auth_mode"`
	OpenAIAPIKey json.RawMessage `json:"OPENAI_API_KEY"`
	Tokens       struct {
		IDToken      *string `json:"id_token"`
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		AccountID    string  `json:"account_id"`
	} `json:"tokens"`
	LastRefresh *string `json:"last_refresh"`
}

func (s *Store) AddAccountWithAuth(label string, contents []byte) (Account, error) {
	normalized, externalID, err := validateImportedAuth(contents)
	if err != nil {
		return Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, account := range s.accounts {
		if authAccountID(filepath.Join(account.CodexHome, "auth.json")) == externalID {
			return Account{}, ErrDuplicateChatGPTAccount
		}
	}
	return s.addAccountLocked(label, normalized)
}

func validateImportedAuth(contents []byte) ([]byte, string, error) {
	if len(contents) == 0 {
		return nil, "", errors.New("auth.json is empty")
	}
	if len(contents) > MaxImportedAuthBytes {
		return nil, "", fmt.Errorf("auth.json exceeds %d bytes", MaxImportedAuthBytes)
	}
	var auth importedAuthDocument
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&auth); err != nil {
		return nil, "", fmt.Errorf("decode auth.json: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return nil, "", err
	}
	if auth.AuthMode != "chatgpt" {
		return nil, "", errors.New("auth.json auth_mode must be chatgpt")
	}
	if len(auth.OpenAIAPIKey) != 0 && string(bytes.TrimSpace(auth.OpenAIAPIKey)) != "null" {
		return nil, "", errors.New("auth.json OPENAI_API_KEY must be null for a ChatGPT subscription")
	}
	for name, value := range map[string]string{
		"access_token":  auth.Tokens.AccessToken,
		"refresh_token": auth.Tokens.RefreshToken,
		"account_id":    auth.Tokens.AccountID,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, "", fmt.Errorf("auth.json tokens.%s must be a non-empty string", name)
		}
	}
	if auth.Tokens.IDToken != nil {
		if strings.TrimSpace(*auth.Tokens.IDToken) == "" {
			return nil, "", errors.New("auth.json tokens.id_token must be a non-empty string when present")
		}
	}
	if auth.LastRefresh != nil {
		if strings.TrimSpace(*auth.LastRefresh) == "" {
			return nil, "", errors.New("auth.json last_refresh must be a non-empty string when present")
		}
		if _, err := time.Parse(time.RFC3339Nano, *auth.LastRefresh); err != nil {
			return nil, "", errors.New("auth.json last_refresh must use RFC 3339 format")
		}
	}
	var normalized bytes.Buffer
	if err := json.Indent(&normalized, bytes.TrimSpace(contents), "", "  "); err != nil {
		return nil, "", fmt.Errorf("format auth.json: %w", err)
	}
	normalized.WriteByte('\n')
	if normalized.Len() > MaxImportedAuthBytes {
		return nil, "", fmt.Errorf("normalized auth.json exceeds %d bytes", MaxImportedAuthBytes)
	}
	return normalized.Bytes(), strings.TrimSpace(auth.Tokens.AccountID), nil
}

// DiscardImportedAccount removes a failed import from routing state and wipes
// its generated account directory, including auth.json and any child-created
// token or database material. It only accepts the exact account-home layout
// created by addAccountLocked.
func (s *Store) DiscardImportedAccount(id string) error {
	s.mu.Lock()
	index := slices.IndexFunc(s.accounts, func(account Account) bool { return account.ID == id })
	if index < 0 {
		s.mu.Unlock()
		return fmt.Errorf("account %q not found", id)
	}
	account := s.accounts[index]
	accountRoot := filepath.Clean(filepath.Join(s.root, "accounts", id))
	if account.Controller || filepath.Clean(filepath.Dir(account.CodexHome)) != accountRoot {
		s.mu.Unlock()
		return fmt.Errorf("account %q is not a disposable imported account", id)
	}
	previousAccounts := slices.Clone(s.accounts)
	previousOwners := maps.Clone(s.owners)
	s.accounts = slices.Delete(s.accounts, index, index+1)
	for threadID, owner := range s.owners {
		if owner == id {
			delete(s.owners, threadID)
		}
	}
	if err := s.saveLocked(); err != nil {
		s.accounts = previousAccounts
		s.owners = previousOwners
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	if err := os.RemoveAll(accountRoot); err != nil {
		return fmt.Errorf("remove imported account resources: %w", err)
	}
	return nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing auth.json content: %w", err)
	}
	return errors.New("auth.json contains more than one JSON value")
}

func authAccountID(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.Size() > MaxImportedAuthBytes {
		return ""
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var auth struct {
		AuthMode string `json:"auth_mode"`
		Tokens   struct {
			AccountID string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(contents, &auth) != nil || auth.AuthMode != "chatgpt" {
		return ""
	}
	return strings.TrimSpace(auth.Tokens.AccountID)
}

func atomicWritePrivate(path string, contents []byte) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".auth.json.tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
