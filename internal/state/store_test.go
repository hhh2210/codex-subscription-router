package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreBootstrapsPrimaryAndPersistsThreadAffinity(t *testing.T) {
	root := t.TempDir()
	primaryHome := filepath.Join(root, "primary")
	store, err := Open(filepath.Join(root, "mux"), primaryHome)
	if err != nil {
		t.Fatal(err)
	}
	accounts := store.Accounts()
	if len(accounts) != 1 || accounts[0].ID != "primary" || !accounts[0].Controller {
		t.Fatalf("unexpected bootstrap accounts: %#v", accounts)
	}
	added, err := store.AddAccount("Work")
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(added.CodexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	wantConfig := "cli_auth_credentials_store = \"file\"\nmcp_oauth_credentials_store = \"file\"\n"
	if string(config) != wantConfig {
		t.Fatalf("unexpected isolated config: %q", config)
	}
	if err := store.SetThreadOwner("thread-1", added.ID); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(filepath.Join(root, "mux"), primaryHome)
	if err != nil {
		t.Fatal(err)
	}
	owner, ok := reopened.ThreadOwner("thread-1")
	if !ok || owner != added.ID {
		t.Fatalf("thread affinity was not persisted: owner=%q ok=%v", owner, ok)
	}
}

func TestAccountConfigInheritsManagedMCPAndPreservesLocalProjects(t *testing.T) {
	root := t.TempDir()
	primaryHome := filepath.Join(root, "primary")
	if err := os.MkdirAll(primaryHome, 0o700); err != nil {
		t.Fatal(err)
	}
	primaryConfig := `model = "gpt-test"

[mcp_servers.node_repl]
command = "/Applications/Codex Subscription Router.app/node_repl"

[mcp_servers.node_repl.env]
SKY_CUA_SERVICE_PATH = "/Applications/Codex Subscription Router Computer Use.app"

[projects."/primary-only"]
trust_level = "trusted"
`
	if err := os.WriteFile(filepath.Join(primaryHome, "config.toml"), []byte(primaryConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	muxRoot := filepath.Join(root, "mux")
	store, err := Open(muxRoot, primaryHome)
	if err != nil {
		t.Fatal(err)
	}
	added, err := store.AddAccount("Work")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(added.CodexHome, "config.toml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	for _, expected := range []string{
		`cli_auth_credentials_store = "file"`,
		`mcp_oauth_credentials_store = "file"`,
		`model = "gpt-test"`,
		`[mcp_servers.node_repl]`,
		`SKY_CUA_SERVICE_PATH = "/Applications/Codex Subscription Router Computer Use.app"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("account config is missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "/primary-only") {
		t.Fatalf("primary project trust leaked into account config:\n%s", text)
	}

	text += `
[projects."/account-project"]
trust_level = "trusted"
`
	if err := os.WriteFile(configPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	primaryConfig = strings.ReplaceAll(primaryConfig, "gpt-test", "gpt-updated")
	if err := os.WriteFile(filepath.Join(primaryHome, "config.toml"), []byte(primaryConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(muxRoot, primaryHome); err != nil {
		t.Fatal(err)
	}
	config, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text = string(config)
	if !strings.Contains(text, `model = "gpt-updated"`) {
		t.Fatalf("managed config was not refreshed:\n%s", text)
	}
	if !strings.Contains(text, `[projects."/account-project"]`) {
		t.Fatalf("account project trust was not preserved:\n%s", text)
	}
}

func TestSyncManagedConfigPropagatesPluginsWithoutRestart(t *testing.T) {
	root := t.TempDir()
	primaryHome := filepath.Join(root, "primary")
	if err := os.MkdirAll(primaryHome, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(primaryHome, "config.toml")
	if err := os.WriteFile(configPath, []byte("model = \"before\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(root, "mux"), primaryHome)
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.AddAccount("Work")
	if err != nil {
		t.Fatal(err)
	}
	updated := "model = \"after\"\n\n[plugins.\"browser@openai-bundled\"]\nenabled = true\n"
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncManagedConfig(); err != nil {
		t.Fatal(err)
	}
	isolated, err := os.ReadFile(filepath.Join(account.CodexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(isolated), `[plugins."browser@openai-bundled"]`) {
		t.Fatalf("plugin config did not propagate:\n%s", isolated)
	}
}

func TestUpdateAccountPreservesController(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root, filepath.Join(root, "primary"))
	if err != nil {
		t.Fatal(err)
	}
	label := "Personal"
	enabled := false
	account, err := store.UpdateAccount("primary", &label, &enabled)
	if err != nil {
		t.Fatal(err)
	}
	if account.Label != label || account.Enabled || !account.Controller {
		t.Fatalf("unexpected updated account: %#v", account)
	}
}

func TestRemoveAccountClearsOwnershipAndPersists(t *testing.T) {
	root := t.TempDir()
	primaryHome := filepath.Join(root, "primary")
	store, err := Open(filepath.Join(root, "mux"), primaryHome)
	if err != nil {
		t.Fatal(err)
	}
	added, err := store.AddAccount("Work")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetThreadOwner("thread-owned", added.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetThreadOwner("thread-primary", "primary"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RemoveAccount("primary"); err == nil {
		t.Fatal("removing the controller account must fail")
	}

	removed, err := store.RemoveAccount(added.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.ID != added.ID {
		t.Fatalf("unexpected removed account: %#v", removed)
	}
	if _, err := store.RemoveAccount(added.ID); err == nil {
		t.Fatal("removing an already removed account must fail")
	}
	for _, account := range store.Accounts() {
		if account.ID == added.ID {
			t.Fatal("removed account is still present in state")
		}
	}
	if owner, ok := store.ThreadOwner("thread-owned"); ok {
		t.Fatalf("thread owned by the removed account kept its owner %q", owner)
	}
	if owner, ok := store.ThreadOwner("thread-primary"); !ok || owner != "primary" {
		t.Fatalf("unrelated thread ownership was disturbed: owner=%q ok=%v", owner, ok)
	}

	reopened, err := Open(filepath.Join(root, "mux"), primaryHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Account(added.ID); ok {
		t.Fatal("removed account reappeared after reopening the store")
	}
}

func TestRemoveAccountRollsBackMemoryAndStageWhenPersistenceFails(t *testing.T) {
	root := t.TempDir()
	muxRoot := filepath.Join(root, "mux")
	store, err := Open(muxRoot, filepath.Join(root, "primary"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.AddAccount("Work")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetThreadOwner("thread-work", account.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(muxRoot, "state.json.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	staged := false
	rolledBack := false
	_, err = store.RemoveAccountWithStage(account.ID, func(Account) (func() error, error) {
		staged = true
		return func() error {
			rolledBack = true
			return nil
		}, nil
	})
	if err == nil {
		t.Fatal("removal unexpectedly succeeded with an unwritable state temporary path")
	}
	if !staged || !rolledBack {
		t.Fatalf("stage lifecycle = staged:%v rolledBack:%v", staged, rolledBack)
	}
	if _, ok := store.Account(account.ID); !ok {
		t.Fatal("account was not restored in memory")
	}
	if owner, ok := store.ThreadOwner("thread-work"); !ok || owner != account.ID {
		t.Fatalf("thread owner was not restored: owner=%q ok=%v", owner, ok)
	}
	reopened, err := Open(muxRoot, filepath.Join(root, "primary"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Account(account.ID); !ok {
		t.Fatal("persisted account was changed despite the failed commit")
	}
}

func TestRemovalSerializesConfigSyncAndPreventsHomeRecreation(t *testing.T) {
	root := t.TempDir()
	primaryHome := filepath.Join(root, "primary")
	if err := os.MkdirAll(primaryHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(primaryHome, "config.toml"), []byte("model = \"shared\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(root, "mux"), primaryHome)
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.AddAccount("Work")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Dir(account.CodexHome)
	destination := filepath.Join(store.Root(), "backups", "staged")
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	stageStarted := make(chan struct{})
	releaseStage := make(chan struct{})
	removeDone := make(chan error, 1)
	go func() {
		_, removeErr := store.RemoveAccountWithStage(account.ID, func(Account) (func() error, error) {
			if err := os.Rename(source, destination); err != nil {
				return nil, err
			}
			close(stageStarted)
			<-releaseStage
			return func() error { return os.Rename(destination, source) }, nil
		})
		removeDone <- removeErr
	}()
	<-stageStarted

	syncStarted := make(chan struct{})
	syncDone := make(chan error, 1)
	go func() {
		close(syncStarted)
		syncDone <- store.SyncManagedConfig()
	}()
	<-syncStarted
	select {
	case err := <-syncDone:
		t.Fatalf("config sync escaped the removal transaction: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseStage)
	if err := <-removeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-syncDone; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("removed account home was recreated: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "codex-home", "config.toml")); err != nil {
		t.Fatalf("staged backup was lost: %v", err)
	}
}
