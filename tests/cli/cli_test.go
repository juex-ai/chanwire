package cli

import (
	"testing"

	"github.com/juex-ai/chanwire/tests/internal/e2e"
)

func TestCLIFlow(t *testing.T) {
	endpoint := e2e.Endpoint()
	bin := e2e.Binary(t)
	suffix := e2e.UniqueSuffix()

	alice := "cli-alice-" + suffix
	bob := "cli-bob-" + suffix
	offlineContent := "cli offline " + suffix
	realtimeContent := "cli realtime " + suffix

	aliceDir := t.TempDir()
	bobDir := t.TempDir()

	out := e2e.RunCLI(t, bin, endpoint, aliceDir, "agent", "register", "--agent_name", alice)
	e2e.AssertContains(t, out, "registered: agent_name="+alice)

	out = e2e.RunCLI(t, bin, endpoint, bobDir, "agent", "register", "--agent_name", bob)
	e2e.AssertContains(t, out, "registered: agent_name="+bob)

	out = e2e.RunCLI(t, bin, endpoint, aliceDir, "agent", "list")
	e2e.AssertContains(t, out, alice)
	e2e.AssertContains(t, out, bob)

	out = e2e.RunCLI(t, bin, endpoint, aliceDir, "msg", "send", "--to_agent", bob, "--content", offlineContent)
	e2e.AssertContains(t, out, "ok: message_id=")

	connect := e2e.StartCLIConnect(t, bin, endpoint, bobDir)
	defer connect.Stop()

	connect.WaitForLine(t, "-- history batch (one-time review, 1 message) --")
	connect.WaitForLine(t, offlineContent)
	connect.WaitForLine(t, "-- end history batch --")
	connect.WaitForLine(t, "-- end of history --")

	out = e2e.RunCLI(t, bin, endpoint, aliceDir, "msg", "send", "--to_agent", bob, "--content", realtimeContent)
	e2e.AssertContains(t, out, "ok: message_id=")

	realtimeLine := connect.WaitForLine(t, realtimeContent)
	e2e.AssertContains(t, realtimeLine, "[realtime] from "+alice)
}
