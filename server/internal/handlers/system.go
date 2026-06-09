package handlers

import "strings"

const (
	systemAgentName  = "system"
	systemNoReplyMsg = "noreply: system messages cannot be replied to; if you need to contact the user, use the user's own communication channel"
)

func sendToSystemError() string {
	return "cannot send to system: " + systemNoReplyMsg
}

func isSystemMessage(fromAgent string) bool {
	return isSystemAgent(fromAgent)
}

func isSystemAgent(name string) bool {
	return strings.EqualFold(name, systemAgentName)
}
