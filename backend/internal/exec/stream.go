package exec

// Stream provides helper types for streaming exec output over WebSocket.

// WSMessage represents a WebSocket message sent to the client.
type WSMessage struct {
	Type    string      `json:"type"`
	JobID   string      `json:"jobId,omitempty"`
	Line    *LogLine    `json:"line,omitempty"`
	Percent int         `json:"percent,omitempty"`
	Phase   string      `json:"phase,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// ErrorEvent is emitted when a known error pattern is detected.
type ErrorEvent struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	RawLine string `json:"raw_line"`
}
