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

func readIndexStyle(t *testing.T) string {
	t.Helper()

	data, err := fs.ReadFile(assets, "dist/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(data)
	start := strings.Index(html, "<style>")
	end := strings.Index(html, "</style>")
	if start < 0 || end < 0 || end <= start {
		t.Fatal("index.html should include an inline style block")
	}
	return html[start+len("<style>") : end]
}

func readIndexScript(t *testing.T) string {
	t.Helper()

	data, err := fs.ReadFile(assets, "dist/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(data)
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
