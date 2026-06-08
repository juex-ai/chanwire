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
	if !ruleDeclares(style, ".message-body", "position:relative") {
		t.Fatal("message cards should wrap content and toggle in a positioned body")
	}
	if !ruleDeclares(style, ".message.collapsed .message-body:after", "width:34%") {
		t.Fatal("collapsed message fade should stay compact instead of blanking too much text before more")
	}
	if !ruleDeclares(style, ".message.collapsed .message-body:after", "background:linear-gradient(90deg,rgba(255,255,255,0),rgba(255,255,255,.86) 68%,var(--white))") {
		t.Fatal("collapsed messages should fade the end of the final visible text line")
	}
	if !ruleDeclares(style, ".message.collapsed .message-toggle", "position:absolute") {
		t.Fatal("collapsed message toggle should sit over the final text line")
	}
	if !ruleDeclares(style, ".message.collapsed .message-toggle", "box-shadow:0 0 0 2px var(--white)") {
		t.Fatal("collapsed message toggle should use a tight halo so the pre-more blank stays small")
	}
	if !ruleDeclares(style, ".message.expanded .message-toggle", "position:static") {
		t.Fatal("expanded message toggle should return to the normal document flow")
	}

	for _, token := range []string{
		"content_html",
		"expandedMessages",
		"renderMessageContent",
		"setupMessageToggles",
		"message-toggle",
		"data-message-id",
		`<div class="message-body"><div class="content">`,
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

func TestOlderMessagesLoadIconContract(t *testing.T) {
	style := readIndexStyle(t)
	script := readIndexScript(t)

	for _, selectorAndDeclaration := range []struct {
		selector    string
		declaration string
	}{
		{".load", "align-self:center"},
		{".load", "width:44px"},
		{".load", "height:44px"},
		{".load", "flex:0 0 44px"},
		{".load", "display:grid"},
		{".load", "place-items:center"},
		{".load:disabled", "cursor:wait"},
	} {
		if !ruleDeclares(style, selectorAndDeclaration.selector, selectorAndDeclaration.declaration) {
			t.Fatalf("%s should declare %s", selectorAndDeclaration.selector, selectorAndDeclaration.declaration)
		}
	}

	for _, token := range []string{
		`aria-label="load older messages"`,
		`>&#8593;</button>`,
		"loadingOlderMessages",
		"if(!first||loadingOlderMessages)return",
		"loadingOlderMessages=true",
		`loadingOlderMessages?'disabled aria-busy="true"':''`,
		"load.setAttribute('aria-busy','true')",
		"loadingOlderMessages=false;renderMessages({preserveTop:true})",
	} {
		if !strings.Contains(script, token) {
			t.Fatalf("web console script should include %q", token)
		}
	}

	if strings.Contains(script, "load older msgs") {
		t.Fatal("load older control should render as an icon, not text")
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
		"render({scrollToBottom:stick,pulseEdge:message})",
		"renderMessages({preserveTop:true})",
		"const data=await r.json()",
		"if(opts.preserveMessages&&state.messages.length)data.messages=state.messages",
		"state=data",
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
	if strings.Contains(script, "hasOlderMessages=hasOlderMessages||") {
		t.Fatal("realtime messages should not re-enable older pagination by growing the visible list")
	}
}

func TestRealtimeMessagesRefreshGraphAndAnimateEdges(t *testing.T) {
	style := readIndexStyle(t)
	script := readIndexScript(t)

	if !ruleDeclares(style, ".edge-pulse .edge-sketch", "animation:edgePulse .86s ease-out") {
		t.Fatal("realtime graph edges should expose a transient pulse animation class")
	}
	if !strings.Contains(style, "@keyframes edgePulse") {
		t.Fatal("edge pulse animation should be declared in CSS")
	}

	for _, token := range []string{
		"function upsertRealtimeGraphMessage",
		"let added=false",
		"state.agents.some(a=>a.agent_name===name)",
		"state.agents.sort((a,b)=>a.agent_name.localeCompare(b.agent_name))",
		"state.edges.some(e=>e.from_agent===message.from_agent&&e.to_agent===message.to_agent)",
		"render({scrollToBottom:stick,pulseEdge:message})",
		"function drawEdges(pos,opts={})",
		"lineClass=pulse?'edge-pulse':''",
	} {
		if !strings.Contains(script, token) {
			t.Fatalf("web console script should include %q", token)
		}
	}

	if strings.Contains(script, "renderMessages({scrollToBottom:stick})}") {
		t.Fatal("realtime messages should redraw the graph, not only the message list")
	}
}

func TestGraphBoardSketchVisualContract(t *testing.T) {
	style := readIndexStyle(t)
	script := readIndexScript(t)

	for _, selectorAndDeclaration := range []struct {
		selector    string
		declaration string
	}{
		{".edge-sketch", "stroke-linecap:round"},
		{".edge-sketch", "stroke-linejoin:round"},
		{".avatar svg", "width:78px"},
		{".avatar svg", "stroke-linecap:round"},
		{".avatar svg", "stroke-linejoin:round"},
		{".avatar-ink", "fill:var(--text)"},
	} {
		if !ruleDeclares(style, selectorAndDeclaration.selector, selectorAndDeclaration.declaration) {
			t.Fatalf("%s should declare %s", selectorAndDeclaration.selector, selectorAndDeclaration.declaration)
		}
	}

	for _, token := range []string{
		"function avatarVariant",
		"function agentAvatar",
		"function sketchEdgePath",
		"agentAvatar(a.agent_name)",
		`class="avatar-ink"`,
		"maxR=Math.max(70,Math.min((w-132)/2,(h-150)/2))",
		"clamp(Math.min(w,h)*.33,92,maxR)",
		"jitter=(seed%7)-3",
		"sx=x2-x1",
		"L ${m1x} ${m1y} L ${m2x} ${m2y}",
		"d2=sketchEdgePath(a,b,p.a+p.b,3)",
		`<path class="${lineClass} edge-sketch"`,
		`marker id="arrow"`,
		`stroke-linejoin="round"`,
	} {
		if !strings.Contains(script, token) {
			t.Fatalf("web console script should include %q", token)
		}
	}

	for _, stale := range []string{
		"function initials",
		`<span class="mono">`,
		`<span class="mouth">`,
		".avatar:before",
		".avatar:after",
		".mouth",
		".mono",
		`<line class=`,
		"d2=sketchEdgePath(a,b,p.b+p.a,10)",
		`fill="#000"`,
		" C ${c1x}",
		"robot",
	} {
		if strings.Contains(script, stale) || strings.Contains(style, stale) {
			t.Fatalf("graph board should remove stale abstract avatar or straight-edge token %q", stale)
		}
	}
}

func TestGraphBoardPanContract(t *testing.T) {
	style := readIndexStyle(t)
	script := readIndexScript(t)

	for _, selectorAndDeclaration := range []struct {
		selector    string
		declaration string
	}{
		{".stage", "cursor:grab"},
		{".stage.dragging", "cursor:grabbing"},
		{"#agents", "transform:translate(var(--pan-x),var(--pan-y))"},
		{"#edges", "transform:translate(var(--pan-x),var(--pan-y))"},
	} {
		if !ruleDeclares(style, selectorAndDeclaration.selector, selectorAndDeclaration.declaration) {
			t.Fatalf("%s should declare %s", selectorAndDeclaration.selector, selectorAndDeclaration.declaration)
		}
	}

	for _, token := range []string{
		"const boardPan={x:0,y:0,dragging:false",
		"function applyBoardPan",
		"function setupBoardPan",
		"stage.addEventListener('pointerdown'",
		"addEventListener('pointermove'",
		"addEventListener('pointerup'",
		"closest('.agent,.composer,button,textarea')",
		"stage.classList.add('dragging')",
		"stage.style.setProperty('--pan-x'",
	} {
		if !strings.Contains(script, token) {
			t.Fatalf("web console script should include %q", token)
		}
	}
}

func TestSmokeBackgroundCanvasContract(t *testing.T) {
	html := readIndexHTML(t)
	style := readIndexStyle(t)
	script := readIndexScript(t)

	if !strings.Contains(html, `<canvas class="smoke" id="smoke"`) {
		t.Fatal("graph board should include an animated smoke canvas layer")
	}
	if strings.Contains(html, `<canvas class="spark" id="spark"`) {
		t.Fatal("graph board should not include a particle canvas layer")
	}
	for _, selectorAndDeclaration := range []struct {
		selector    string
		declaration string
	}{
		{".board", "background:linear-gradient(135deg,#f8fcff 0%,#f5f8ff 46%,#fbfbf8 100%)"},
		{".smoke", "position:absolute"},
		{".smoke", "filter:blur(16px) saturate(1.28) contrast(1.04)"},
		{".smoke", "pointer-events:none"},
	} {
		if !ruleDeclares(style, selectorAndDeclaration.selector, selectorAndDeclaration.declaration) {
			t.Fatalf("%s should declare %s", selectorAndDeclaration.selector, selectorAndDeclaration.declaration)
		}
	}
	if got := strings.Count(script, "rgb:'"); got < 5 {
		t.Fatalf("smoke background should render at least five color clouds, got %d", got)
	}

	for _, stale := range []string{
		"radial-gradient(circle at 15% 20%",
		"radial-gradient(circle at 88% 18%",
		"radial-gradient(circle at 65% 84%",
		"ctx.globalCompositeOperation='lighter'",
		"rgba(255,255,255,${mix})",
		"255,111,89",
		"255,209,102",
		"if(b.x<.08||b.x>.92)b.vx*=-1",
		"ctx.arc(px,py,size,0",
		"for(let j=0;j<26;j++)",
		"const pts=[]",
		".spark",
		"const smokeParticles=",
		"const sparkDust=",
		"const smokeGrain=",
		"function drawSparkParticles",
		"sparkCanvas",
		"sparkCtx",
		"smokeParticles.forEach",
		"sparkDust.forEach",
		"smokeGrain.forEach",
		"drawSparkParticles(",
	} {
		if strings.Contains(style, stale) {
			t.Fatalf("graph board should remove stale static color-ball background %q", stale)
		}
		if strings.Contains(script, stale) {
			t.Fatalf("graph board should remove stale smoke implementation %q", stale)
		}
	}

	for _, token := range []string{
		"const smokeAnchors=[",
		"const smokeBlobs=smokeAnchors.map",
		"flow:.88+i*.09",
		"homeX:a.x",
		"homeY:a.y",
		"const organicPts=Array.from({length:14},()=>({x:0,y:0}))",
		"function setupSmokeCanvas",
		"function resizeLayer",
		"function animateSmoke",
		"function organicSmokePath",
		"function mixRGB",
		"const mouseInfluence=smokeMouse.active ? 0.35 : 0",
		"smokeTick+=.0062",
		"ctx.globalCompositeOperation='screen'",
		"for(let j=0;j<14;j++)",
		"dist/(span*.62)",
		"size=span*.3",
		"rgba(${rgb},${mix*.45})",
		"for(let k=0;k<14;k++)",
		"const p=organicPts[k]",
		"ctx.quadraticCurveTo",
		"mixRGB(a.rgb,b.rgb)",
		"requestAnimationFrame(animateSmoke)",
		"stage.addEventListener('pointermove',ev=>",
		"smokeMouse.windX+=(x-smokeMouse.lastX)*.006",
	} {
		if !strings.Contains(script, token) {
			t.Fatalf("web console script should include %q", token)
		}
	}

	animateStart := strings.Index(script, "function animateSmoke")
	if animateStart < 0 {
		t.Fatal("web console script should expose an animateSmoke body before connect")
	}
	connectStart := strings.Index(script[animateStart:], "function connect")
	if connectStart < 0 {
		t.Fatal("web console script should expose an animateSmoke body before connect")
	}
	animateBody := script[animateStart : animateStart+connectStart]
	if strings.Contains(animateBody, "ctx.fillRect") || strings.Contains(animateBody, "ctx.arc") {
		t.Fatal("smoke animation should use only organic cloud paths, not particle dots")
	}
}

func TestWebConsoleMicrointeractionContract(t *testing.T) {
	style := readIndexStyle(t)

	for _, selectorAndDeclaration := range []struct {
		selector    string
		declaration string
	}{
		{".agent", "transition:transform .18s ease,filter .18s ease"},
		{".agent:hover", "transform:translate(-50%,calc(-50% - 8px)) scale(1.04)"},
		{".agent:active", "transform:translate(-50%,calc(-50% - 3px)) scale(1.01)"},
		{".agent:hover .avatar", "box-shadow:0 12px 0 rgba(0,0,0,.14)"},
		{".agent:focus-visible .avatar", "outline:3px solid var(--blue)"},
		{".agent:focus-visible .avatar", "outline-offset:4px"},
		{".composer button", "transition:transform .16s ease,box-shadow .16s ease,filter .16s ease"},
		{".load", "transition:transform .16s ease,box-shadow .16s ease,filter .16s ease"},
		{".jump-latest", "transition:transform .16s ease,box-shadow .16s ease,filter .16s ease"},
		{".message-toggle", "transition:transform .16s ease,filter .16s ease"},
		{".composer button:hover", "transform:translate(-2px,-2px)"},
		{".load:hover", "transform:translate(-2px,-2px)"},
		{".jump-latest:hover", "transform:translate(-2px,-2px)"},
		{".composer button:active", "transform:translate(1px,1px)"},
		{".load:active", "transform:translate(1px,1px)"},
		{".jump-latest:active", "transform:translate(1px,1px)"},
		{".load:disabled:hover", "transform:none"},
		{".load:disabled:hover", "box-shadow:2px 2px 0 var(--text)"},
		{".composer button:disabled", "opacity:.65"},
		{".composer button:disabled", "cursor:not-allowed"},
		{".composer button:disabled:hover", "transform:none"},
		{".composer button:disabled:hover", "box-shadow:2px 2px 0 var(--text)"},
		{".route-agent", "transition:transform .14s ease,filter .14s ease"},
		{".route-agent:hover", "transform:translateY(-1px)"},
		{".message-toggle:hover", "transform:translateY(-1px)"},
	} {
		if !ruleDeclares(style, selectorAndDeclaration.selector, selectorAndDeclaration.declaration) {
			t.Errorf("%s should declare %s", selectorAndDeclaration.selector, selectorAndDeclaration.declaration)
		}
	}

	for _, selector := range []string{
		".composer button:focus-visible",
		".load:focus-visible",
		".jump-latest:focus-visible",
		".message-toggle:focus-visible",
	} {
		if !ruleDeclares(style, selector, "outline:3px solid var(--blue)") {
			t.Errorf("%s should show a visible keyboard focus ring", selector)
		}
		if !ruleDeclares(style, selector, "outline-offset:3px") {
			t.Errorf("%s should offset the keyboard focus ring", selector)
		}
	}
}

func TestSettingsAgentAdminPageContract(t *testing.T) {
	html := readIndexHTML(t)
	style := readIndexStyle(t)
	script := readIndexScript(t)

	for _, token := range []string{
		`id="home-page"`,
		`id="settings-link" href="/settings"`,
		`aria-label="open settings"`,
		`id="settings-page" hidden`,
		`id="settings-back" href="/"`,
		`data-settings-group="agents"`,
		`id="settings-agents"`,
		`id="settings-load"`,
	} {
		if !strings.Contains(html, token) {
			t.Fatalf("settings page should include %q", token)
		}
	}
	if strings.Contains(html, `<div class="status" id="status"></div><a class="icon-button settings-link"`) {
		t.Fatal("settings link should not sit inside the left stage header")
	}

	for _, selectorAndDeclaration := range []struct {
		selector    string
		declaration string
	}{
		{".settings", "display:grid"},
		{".settings-body", "grid-template-columns:220px minmax(0,1fr)"},
		{".settings-nav", "border-right:2px solid var(--text)"},
		{".settings-nav button.active", "background:var(--lemon)"},
		{".agents-table", "width:100%"},
		{".agents-table", "border-collapse:separate"},
		{".danger", "background:var(--pumpkin)"},
		{".settings-load", "background:var(--lemon)"},
		{".settings-link", "position:absolute"},
		{".settings-link", "right:24px"},
		{".settings-link", "top:20px"},
		{".settings-link", "z-index:4"},
	} {
		if !ruleDeclares(style, selectorAndDeclaration.selector, selectorAndDeclaration.declaration) {
			t.Fatalf("%s should declare %s", selectorAndDeclaration.selector, selectorAndDeclaration.declaration)
		}
	}

	for _, token := range []string{
		"const settingsPageSize=20",
		"let homeNeedsRefresh=false",
		"function showRoute",
		"location.pathname==='/settings'",
		"function refreshHomeAfterVisible",
		"requestAnimationFrame(()=>loadState({preserveMessages:true}))",
		"else{requestAnimationFrame(()=>render({scrollToBottom:isNearMessagesBottom()}));if(homeNeedsRefresh){homeNeedsRefresh=false;refreshHomeAfterVisible()}}",
		"if(homeNeedsRefresh){homeNeedsRefresh=false;refreshHomeAfterVisible()}",
		"history.pushState",
		"addEventListener('popstate',showRoute)",
		"function loadSettingsAgents",
		"`/api/v1/web/settings/agents?limit=${settingsPageSize}&offset=${settingsOffset}`",
		"function renderSettingsAgents",
		"related_agent_count",
		"agent.active?'active':'offline'",
		"function deleteSettingsAgent",
		"method:'DELETE'",
		"encodeURIComponent(name)",
		"homeNeedsRefresh=true",
	} {
		if !strings.Contains(script, token) {
			t.Fatalf("settings script should include %q", token)
		}
	}

	deleteStart := strings.Index(script, "async function deleteSettingsAgent")
	if deleteStart < 0 {
		t.Fatal("settings script should include deleteSettingsAgent")
	}
	deleteEnd := strings.Index(script[deleteStart:], "stage.addEventListener")
	if deleteEnd < 0 {
		t.Fatal("settings script should keep deleteSettingsAgent before stage setup")
	}
	deleteScript := script[deleteStart : deleteStart+deleteEnd]
	if strings.Contains(deleteScript, "loadState({preserveMessages:true})") {
		t.Fatal("settings delete should defer home state reload until the home page is visible")
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
