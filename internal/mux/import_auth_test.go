package mux

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/b-nnett/codex-subscription-router/internal/state"
)

const importedAuthFixture = `{
  "auth_mode": "chatgpt",
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "id-token-test",
    "access_token": "access-token-test",
    "refresh_token": "refresh-token-test",
    "account_id": "account-import-test"
  },
  "last_refresh": "2026-08-24T00:00:00Z"
}`

func TestImportAccountAuthenticatesOnFirstChildStart(t *testing.T) {
	root := t.TempDir()
	store, err := state.Open(filepath.Join(root, "mux"), filepath.Join(root, "primary"))
	if err != nil {
		t.Fatal(err)
	}
	multiplexer, err := New(Options{
		RealExecutable: os.Args[0],
		RealArgs:       []string{"-test.run=TestImportedAuthHelperProcess"},
		Environment:    append(os.Environ(), "CODEX_MUX_AUTH_HELPER=connected"),
		Store:          store,
		Output:         io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer multiplexer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot, err := multiplexer.ImportAccount(ctx, "Imported", json.RawMessage(importedAuthFixture))
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Connected || snapshot.AuthType != "chatgpt" || snapshot.PlanLabel != "Plus" {
		t.Fatalf("unexpected imported account snapshot: %#v", snapshot)
	}
	account, ok := store.Account(snapshot.ID)
	if !ok {
		t.Fatalf("imported account %q missing from store", snapshot.ID)
	}
	info, err := os.Stat(filepath.Join(account.CodexHome, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("auth.json mode = %#o, want 0600", info.Mode().Perm())
	}
}

func TestImportAccountRollsBackStartupAndVerificationFailures(t *testing.T) {
	tests := []struct {
		name       string
		executable string
		helperMode string
		initialize bool
	}{
		{name: "process start", executable: "missing-app-server"},
		{name: "initialize", executable: os.Args[0], helperMode: "initialize-error", initialize: true},
		{name: "disconnected verification", executable: os.Args[0], helperMode: "disconnected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := state.Open(filepath.Join(root, "mux"), filepath.Join(root, "primary"))
			if err != nil {
				t.Fatal(err)
			}
			executable := test.executable
			args := []string(nil)
			environment := os.Environ()
			if test.helperMode != "" {
				args = []string{"-test.run=TestImportedAuthHelperProcess"}
				environment = append(environment, "CODEX_MUX_AUTH_HELPER="+test.helperMode)
			} else {
				executable = filepath.Join(root, executable)
			}
			multiplexer, err := New(Options{
				RealExecutable: executable,
				RealArgs:       args,
				Environment:    environment,
				Store:          store,
				Output:         io.Discard,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer multiplexer.Close()
			if test.initialize {
				multiplexer.initializeParams = json.RawMessage(`{"clientInfo":{"name":"test"}}`)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := multiplexer.ImportAccount(ctx, "Rejected", json.RawMessage(importedAuthFixture)); err == nil {
				t.Fatal("failed import unexpectedly succeeded")
			}
			if accounts := store.Accounts(); len(accounts) != 1 || accounts[0].ID != "primary" {
				t.Fatalf("accounts after rollback = %#v", accounts)
			}
			multiplexer.childrenMu.RLock()
			childCount := len(multiplexer.children)
			multiplexer.childrenMu.RUnlock()
			if childCount != 0 {
				t.Fatalf("child count after rollback = %d, want 0", childCount)
			}
			entries, err := os.ReadDir(filepath.Join(store.Root(), "accounts"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("account resources remain after rollback: %v", entries)
			}
			reopened, err := state.Open(store.Root(), filepath.Join(root, "primary"))
			if err != nil {
				t.Fatal(err)
			}
			if accounts := reopened.Accounts(); len(accounts) != 1 || accounts[0].ID != "primary" {
				t.Fatalf("persisted accounts after rollback = %#v", accounts)
			}
		})
	}
}

func TestImportedAuthHelperProcess(t *testing.T) {
	mode := os.Getenv("CODEX_MUX_AUTH_HELPER")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		result := any(map[string]any{})
		if request.Method == "initialize" && mode == "initialize-error" {
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"error": map[string]any{"code": -32000, "message": "initialize rejected"},
			})
			continue
		}
		switch request.Method {
		case "account/read":
			authPath := filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")
			if mode == "disconnected" {
				result = map[string]any{"account": nil}
			} else if _, err := os.Stat(authPath); err != nil {
				result = map[string]any{"account": nil}
			} else {
				result = map[string]any{"account": map[string]any{
					"type": "chatgpt", "email": "import@example.com", "planType": "plus",
				}}
			}
		case "account/rateLimits/read":
			result = map[string]any{"rateLimits": map[string]any{
				"primary": map[string]any{"usedPercent": 10},
			}}
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
	os.Exit(0)
}
