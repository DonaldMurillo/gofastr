package user

import "a2a"

// The marker crosses the package boundary as a fact.
func reply(parts []a2a.Part) a2a.Message {
	return a2a.Message{MessageID: "m", Parts: parts} // want `Parts is a required JSON array`
}

// So does the helper's non-nil fact.
func replyFixed(parts []a2a.Part) a2a.Message {
	return a2a.Message{MessageID: "m", Parts: a2a.PartsOrEmpty(parts)}
}
