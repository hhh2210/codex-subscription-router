package mux

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/b-nnett/codex-subscription-router/internal/backend"
	"github.com/b-nnett/codex-subscription-router/internal/protocol"
)

func TestRemoveAccountFailsInFlightRPCAndDiscardsLateEvents(t *testing.T) {
	store, account, m := newRunningRemovalMultiplexer(t)
	var output bytes.Buffer
	m.output = &output
	m.pendingLogins[account.ID] = true
	request := protocol.Request("thread/read", protocol.StringID("pending-request"), json.RawMessage(`{"threadId":"chat"}`))
	if err := m.forward(account.ID, request); err != nil {
		t.Fatal(err)
	}
	other := protocol.Request("thread/read", protocol.StringID("other-request"), nil)
	m.externalRoutes[protocol.RequestIDKey(other.ID)] = externalRoute{accountID: "primary", message: other}
	if err := m.RemoveAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	var response protocol.Message
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	if string(response.ID) != string(request.ID) || response.Error == nil {
		t.Fatalf("wrong teardown response: %+v", response)
	}
	if len(m.externalRoutes) != 1 {
		t.Fatal("removal failed to isolate this account's pending RPCs")
	}
	if len(m.pendingLogins) != 0 {
		t.Fatal("removed account kept its pending login")
	}
	size := output.Len()
	m.handleInbound(backend.Inbound{AccountID: account.ID, Message: protocol.Success(request.ID, json.RawMessage(`{"thread":{"id":"late-chat"}}`))})
	m.handleInbound(backend.Inbound{AccountID: account.ID, Message: protocol.Message{Method: "thread/started", Params: json.RawMessage(`{"thread":{"id":"late-chat"}}`)}})
	if _, ok := store.ThreadOwner("late-chat"); ok {
		t.Fatal("late event restored removed ownership")
	}
	if output.Len() != size {
		t.Fatal("late response/notification was forwarded")
	}
	if err := store.SetThreadOwner("late-direct", account.ID); err == nil {
		t.Fatal("store accepted a missing owner")
	}
}
