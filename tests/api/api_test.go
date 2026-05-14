package api

import (
	"net/http"
	"testing"

	"github.com/juex-ai/chanwire/tests/internal/e2e"
)

func TestServerAPIFlow(t *testing.T) {
	endpoint := e2e.Endpoint()
	suffix := e2e.UniqueSuffix()

	alice := "api-alice-" + suffix
	bob := "api-bob-" + suffix
	offlineContent := "api offline " + suffix
	realtimeContent := "api realtime " + suffix

	aliceToken := e2e.RegisterAgent(t, endpoint, alice)
	bobToken := e2e.RegisterAgent(t, endpoint, bob)
	bobTokenAgain := e2e.RegisterAgent(t, endpoint, bob)
	if bobTokenAgain != bobToken {
		t.Fatalf("register should be idempotent for %q: got different tokens", bob)
	}

	agents := e2e.ListAgents(t, endpoint, aliceToken)
	e2e.AssertAgentPresent(t, agents, alice)
	e2e.AssertAgentPresent(t, agents, bob)

	e2e.SendMessage(t, endpoint, aliceToken, bob, offlineContent, http.StatusOK)

	conn := e2e.DialWS(t, endpoint, bobToken)
	defer conn.Close()

	if !e2e.ReadUntilHistoryDone(t, conn, offlineContent) {
		t.Fatalf("websocket history did not include %q", offlineContent)
	}

	e2e.SendMessage(t, endpoint, aliceToken, bob, realtimeContent, http.StatusOK)
	frame := e2e.ReadMatchingFrame(t, conn, "realtime", realtimeContent)
	if frame.FromAgent != alice {
		t.Fatalf("realtime from_agent: got %q want %q", frame.FromAgent, alice)
	}

	e2e.SendMessage(t, endpoint, aliceToken, "missing-"+suffix, "nope", http.StatusNotFound)
}
