package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestWebConsoleWrapsLongGeneratedText(t *testing.T) {
	style := readIndexStyle(t)
	script := readIndexScript(t)

	for _, selector := range []string{".message", ".route", ".content", ".composer h3", ".toast"} {
		if !ruleDeclares(style, selector, "overflow-wrap:anywhere") {
			t.Errorf("%s should allow long generated text to wrap inside its container", selector)
		}
	}

	for _, selector := range []string{".messages", ".message"} {
		if !ruleDeclares(style, selector, "min-width:0") {
			t.Errorf("%s should be allowed to shrink instead of forcing horizontal page overflow", selector)
		}
	}

	if !strings.Contains(script, "replace(/\\n/g,'<br>')") {
		t.Error("plain-text fallback content should preserve intentional message line breaks")
	}
}

func TestRuleDeclaresFindsSelectorInsideMediaBlock(t *testing.T) {
	style := "@media(max-width:900px){.app{min-width:0}.feed{overflow-wrap:anywhere}}"

	if !ruleDeclares(style, ".app", "min-width:0") {
		t.Fatal("ruleDeclares should find the first selector inside a media block")
	}
	if !ruleDeclares(style, ".feed", "overflow-wrap:anywhere") {
		t.Fatal("ruleDeclares should find later selectors inside a media block")
	}
}

func TestComposerBubbleInteractionContract(t *testing.T) {
	style := readIndexStyle(t)
	script := readIndexScript(t)

	if !ruleDeclares(style, ".composer button", "margin-left:auto") {
		t.Fatal("composer send button should align to the right")
	}

	for _, token := range []string{
		"function placeComposer",
		"const placements",
		"stage.addEventListener",
		"contains(ev.target)",
		"ev.stopPropagation",
		"button.disabled=true",
		"catch",
		"to ${esc(name)}:",
	} {
		if !strings.Contains(script, token) {
			t.Fatalf("web console script should include %q", token)
		}
	}
}

func TestMessageCardsRenderMarkdownRouteChipsAndCollapse(t *testing.T) {
	style := readIndexStyle(t)
	script := readIndexScript(t)

	if !ruleDeclares(style, ".route-agent", "background:var(--agent-color)") {
		t.Fatal("message route agents should render as colored chips")
	}
	if !ruleDeclares(style, ".message.collapsed .content", "-webkit-line-clamp:5") {
		t.Fatal("collapsed message content should clamp to five lines")
	}

	for _, token := range []string{
		"content_html",
		"expandedMessages",
		"renderMessageContent",
		"setupMessageToggles",
		"message-toggle",
		"data-message-id",
		"isExpanded",
		"visible.has(id)",
		"card.dataset.messageId",
		"aria-expanded",
		"--agent-color:${color(m.from_agent)}",
		"--agent-color:${color(m.to_agent)}",
	} {
		if !strings.Contains(script, token) {
			t.Fatalf("web console script should include %q", token)
		}
	}
}

func TestMessageListLayoutAndOlderPaginationContract(t *testing.T) {
	html := readIndexHTML(t)
	style := readIndexStyle(t)
	script := readIndexScript(t)

	if !ruleDeclares(style, ".app", "grid-template-columns:minmax(360px,1fr) minmax(390px,585px)") {
		t.Fatal("desktop layout should make the message feed roughly 1.5x wider without forcing fixed overflow")
	}
	if !ruleDeclares(style, ".stage", "min-width:0") {
		t.Fatal("stage should be allowed to shrink beside the wider message feed")
	}
	if strings.Contains(html, "feed-sub") || strings.Contains(html, "system-wide live feed") {
		t.Fatal("messages header should not render the old subtitle")
	}
	if strings.Contains(html, `<button class="load" id="load">`) {
		t.Fatal("load older control should be rendered inside the scrollable message list, not fixed below the header")
	}

	for _, token := range []string{
		"renderOlderMessagesControl()",
		"setupOlderMessagesControl",
		"function loadOlderMessages",
		"const messagePageSize=20",
		"before_id=${first}",
		"load.disabled=true",
		"catch{",
		"toast('load failed')",
		"state.messages=[...data.messages,...state.messages]",
		"data.messages.length===0",
		"data.messages.length<messagePageSize",
		"state.messages.length>=messagePageSize",
	} {
		if !strings.Contains(script, token) {
			t.Fatalf("web console script should include %q", token)
		}
	}
}

func TestMessageListScrollToLatestContract(t *testing.T) {
	html := readIndexHTML(t)
	style := readIndexStyle(t)
	script := readIndexScript(t)

	for _, token := range []string{
		`id="jump-latest"`,
		`aria-label="scroll to latest"`,
		`hidden>&#8595;</button>`,
	} {
		if !strings.Contains(html, token) {
			t.Fatalf("web console should include jump-to-latest control token %q", token)
		}
	}

	if !ruleDeclares(style, ".feed", "position:relative") {
		t.Fatal("message feed should anchor the floating jump-to-latest control")
	}
	if !ruleDeclares(style, ".jump-latest", "position:absolute") {
		t.Fatal("jump-to-latest control should float over the message feed")
	}
	if !ruleDeclares(style, ".jump-latest[hidden]", "display:none") {
		t.Fatal("jump-to-latest control should be hidden while already near the latest message")
	}

	for _, token := range []string{
		"const bottomStickThreshold=48",
		"function isNearMessagesBottom",
		"function scrollMessagesToBottom",
		"function updateJumpToLatest",
		"function appendRealtimeMessage",
		"function setupMessageScrollControls",
		"renderMessages({scrollToBottom:stick})",
		"renderMessages({preserveTop:true})",
		"loadState({scrollToBottom:true})",
		"$('messages').addEventListener('scroll',updateJumpToLatest",
		"$('jump-latest').onclick=scrollMessagesToBottom",
	} {
		if !strings.Contains(script, token) {
			t.Fatalf("web console script should include %q", token)
		}
	}

	if strings.Contains(script, "state.messages=[...state.messages,f.message].slice(-80);loadState()") {
		t.Fatal("realtime messages should not immediately refetch state and lose scroll intent")
	}
}

func readIndexHTML(t *testing.T) string {
	t.Helper()

	data, err := fs.ReadFile(assets, "dist/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	return string(data)
}

func readIndexStyle(t *testing.T) string {
	t.Helper()

	html := readIndexHTML(t)
	start := strings.Index(html, "<style>")
	end := strings.Index(html, "</style>")
	if start < 0 || end < 0 || end <= start {
		t.Fatal("index.html should include an inline style block")
	}
	return html[start+len("<style>") : end]
}

func readIndexScript(t *testing.T) string {
	t.Helper()

	html := readIndexHTML(t)
	start := strings.Index(html, "<script>")
	end := strings.Index(html, "</script>")
	if start < 0 || end < 0 || end <= start {
		t.Fatal("index.html should include an inline script block")
	}
	return html[start+len("<script>") : end]
}

func ruleDeclares(style, selector, declaration string) bool {
	return blockDeclares(style, selector, compactCSS(declaration))
}

func blockDeclares(style, selector, declaration string) bool {
	for {
		open := strings.Index(style, "{")
		if open < 0 {
			return false
		}
		close := matchingBrace(style, open)
		if close < 0 {
			return false
		}

		ruleSelector := strings.TrimSpace(style[:open])
		ruleBody := style[open+1 : close]
		if strings.HasPrefix(ruleSelector, "@") {
			if blockDeclares(ruleBody, selector, declaration) {
				return true
			}
		} else if selectorListContains(ruleSelector, selector) && strings.Contains(compactCSS(ruleBody), declaration) {
			return true
		}

		style = style[close+1:]
	}
}

func matchingBrace(style string, open int) int {
	depth := 0
	for i := open; i < len(style); i++ {
		switch style[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func selectorListContains(raw, selector string) bool {
	for _, candidate := range strings.Split(raw, ",") {
		if strings.TrimSpace(candidate) == selector {
			return true
		}
	}
	return false
}

func compactCSS(s string) string {
	replacer := strings.NewReplacer(" ", "", "\n", "", "\t", "")
	return replacer.Replace(s)
}
