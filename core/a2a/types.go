package a2a

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProtocolVersion is the A2A protocol version this package speaks, in the
// Major.Minor form the agent card's supportedInterfaces expects.
const ProtocolVersion = "1.0"

// JSON-RPC method names of the A2A v1.0 JSON-RPC binding. Pinned as the
// PascalCase RPC names from specification/a2a.proto (§5.3 method mapping);
// the v0.x slash forms are wire-incompatible, not merely outdated.
const (
	MethodSendMessage                      = "SendMessage"
	MethodSendStreamingMessage             = "SendStreamingMessage"
	MethodGetTask                          = "GetTask"
	MethodListTasks                        = "ListTasks"
	MethodCancelTask                       = "CancelTask"
	MethodSubscribeToTask                  = "SubscribeToTask"
	MethodCreateTaskPushNotificationConfig = "CreateTaskPushNotificationConfig"
	MethodGetTaskPushNotificationConfig    = "GetTaskPushNotificationConfig"
	MethodListTaskPushNotificationConfigs  = "ListTaskPushNotificationConfigs"
	MethodDeleteTaskPushNotificationConfig = "DeleteTaskPushNotificationConfig"
	MethodGetExtendedAgentCard             = "GetExtendedAgentCard"
)

// Methods lists every JSON-RPC method the server dispatches, in spec order.
var Methods = []string{
	MethodSendMessage, MethodSendStreamingMessage, MethodGetTask, MethodListTasks,
	MethodCancelTask, MethodSubscribeToTask, MethodCreateTaskPushNotificationConfig,
	MethodGetTaskPushNotificationConfig, MethodListTaskPushNotificationConfigs,
	MethodDeleteTaskPushNotificationConfig, MethodGetExtendedAgentCard,
}

// TaskState is the lifecycle state of a task. Values serialize as the
// proto enum names (ProtoJSON), e.g. "TASK_STATE_COMPLETED".
type TaskState string

const (
	TaskStateUnspecified   TaskState = "TASK_STATE_UNSPECIFIED"
	TaskStateSubmitted     TaskState = "TASK_STATE_SUBMITTED"
	TaskStateWorking       TaskState = "TASK_STATE_WORKING"
	TaskStateCompleted     TaskState = "TASK_STATE_COMPLETED"
	TaskStateFailed        TaskState = "TASK_STATE_FAILED"
	TaskStateCanceled      TaskState = "TASK_STATE_CANCELED"
	TaskStateInputRequired TaskState = "TASK_STATE_INPUT_REQUIRED"
	TaskStateRejected      TaskState = "TASK_STATE_REJECTED"
	TaskStateAuthRequired  TaskState = "TASK_STATE_AUTH_REQUIRED"
)

// Terminal reports whether the state ends a task: no further messages are
// accepted and no further updates are emitted.
func (s TaskState) Terminal() bool {
	switch s {
	case TaskStateCompleted, TaskStateFailed, TaskStateCanceled, TaskStateRejected:
		return true
	}
	return false
}

// Interrupted reports whether the task is paused waiting on the client:
// input or authentication. A message addressed to an interrupted task
// resumes it; a stream closes when a task enters one of these states.
func (s TaskState) Interrupted() bool {
	return s == TaskStateInputRequired || s == TaskStateAuthRequired
}

// Valid reports whether s is one of the nine proto enum values.
func (s TaskState) Valid() bool {
	switch s {
	case TaskStateUnspecified, TaskStateSubmitted, TaskStateWorking, TaskStateCompleted,
		TaskStateFailed, TaskStateCanceled, TaskStateInputRequired, TaskStateRejected,
		TaskStateAuthRequired:
		return true
	}
	return false
}

// Role is the author of a message; serializes as "ROLE_USER" / "ROLE_AGENT".
type Role string

const (
	RoleUnspecified Role = "ROLE_UNSPECIFIED"
	RoleUser        Role = "ROLE_USER"
	RoleAgent       Role = "ROLE_AGENT"
)

// Message is one turn of the exchange, from the client (RoleUser) or the
// agent (RoleAgent).
type Message struct {
	MessageID        string         `json:"messageId"`
	ContextID        string         `json:"contextId,omitempty"`
	TaskID           string         `json:"taskId,omitempty"`
	Role             Role           `json:"role"`
	Parts            []Part         `json:"parts"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	Extensions       []string       `json:"extensions,omitempty"`
	ReferenceTaskIDs []string       `json:"referenceTaskIds,omitempty"`
}

// Part is one unit of content in a message or artifact. Exactly one of
// Text, Raw, URL, or Data is set; the JSON form is flat, with the set field
// as the discriminator: {"text": "…"}, {"raw": "<base64>"}, {"url": "…"},
// {"data": <any JSON value>}. Filename, MediaType and Metadata are
// optional on every kind.
//
// Data is a pointer so that a JSON null or false or 0 data payload is
// distinguishable from "no data part": the pointer is non-nil whenever the
// key was present.
type Part struct {
	Text      *string        `json:"text,omitempty"`
	Raw       []byte         `json:"raw,omitempty"`
	URL       *string        `json:"url,omitempty"`
	Data      *any           `json:"data,omitempty"`
	Filename  string         `json:"filename,omitempty"`
	MediaType string         `json:"mediaType,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TextPart builds a text part.
func TextPart(s string) Part { return Part{Text: &s} }

// DataPart builds a structured-data part. MediaType defaults to
// application/json at the caller's discretion; the spec leaves it optional.
func DataPart(v any) Part { return Part{Data: &v} }

// URLPart builds a file-by-reference part.
func URLPart(u, filename, mediaType string) Part {
	return Part{URL: &u, Filename: filename, MediaType: mediaType}
}

// RawPart builds a file-by-value part; the bytes serialize as base64.
func RawPart(b []byte, filename, mediaType string) Part {
	return Part{Raw: b, Filename: filename, MediaType: mediaType}
}

// Kind returns which content field the part carries: "text", "raw", "url",
// "data", or "" when none is set (an invalid part).
func (p Part) Kind() string {
	switch {
	case p.Text != nil:
		return "text"
	case p.Raw != nil:
		return "raw"
	case p.URL != nil:
		return "url"
	case p.Data != nil:
		return "data"
	}
	return ""
}

// Validate enforces the exactly-one-content rule the spec states for Part.
func (p Part) Validate() error {
	n := 0
	for _, set := range []bool{p.Text != nil, p.Raw != nil, p.URL != nil, p.Data != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("a2a: part must carry exactly one of text, raw, url, data; got %d", n)
	}
	return nil
}

// UnmarshalJSON decodes a part and enforces the exactly-one-content rule,
// so a malformed client part is refused at the boundary rather than
// reaching a skill as an empty Part.
func (p *Part) UnmarshalJSON(b []byte) error {
	type plain Part
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	// A present-but-null "raw" decodes to a nil slice, indistinguishable
	// from absent; that is acceptable — null raw is not a valid part either.
	*p = Part(v)
	return p.Validate()
}

// Artifact is an output produced by a task.
type Artifact struct {
	ArtifactID  string         `json:"artifactId"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parts       []Part         `json:"parts"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Extensions  []string       `json:"extensions,omitempty"`
}

// TaskStatus is the current state of a task plus the agent's most recent
// status message, if any.
type TaskStatus struct {
	State     TaskState  `json:"state"`
	Message   *Message   `json:"message,omitempty"`
	Timestamp *Timestamp `json:"timestamp,omitempty"`
}

// Timestamp serializes as RFC 3339 in UTC with millisecond precision, the
// form the spec shows ("2025-10-28T14:25:33.142Z"). Decoding accepts any
// RFC 3339 string.
type Timestamp struct{ time.Time }

// Now returns the current time as a Timestamp, truncated to milliseconds so
// a value survives a JSON round trip byte-for-byte.
func Now() *Timestamp {
	return &Timestamp{time.Now().UTC().Truncate(time.Millisecond)}
}

// MarshalJSON implements json.Marshaler.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.UTC().Format("2006-01-02T15:04:05.000Z07:00"))
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *Timestamp) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("a2a: timestamp %q: %w", s, err)
	}
	t.Time = parsed.UTC()
	return nil
}

// Task is the unit of work a client and an agent exchange messages about.
type Task struct {
	ID        string         `json:"id"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	History   []Message      `json:"history,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskStatusUpdateEvent announces a state change on a streaming connection
// or a push notification.
type TaskStatusUpdateEvent struct {
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskArtifactUpdateEvent announces a new or extended artifact.
type TaskArtifactUpdateEvent struct {
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Artifact  Artifact       `json:"artifact"`
	Append    bool           `json:"append,omitempty"`
	LastChunk bool           `json:"lastChunk,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// StreamResponse is the streaming result and push-notification payload: an
// object carrying exactly one of task / message / statusUpdate /
// artifactUpdate. Construct with one field set; Validate rejects the rest.
type StreamResponse struct {
	Task           *Task                    `json:"task,omitempty"`
	Message        *Message                 `json:"message,omitempty"`
	StatusUpdate   *TaskStatusUpdateEvent   `json:"statusUpdate,omitempty"`
	ArtifactUpdate *TaskArtifactUpdateEvent `json:"artifactUpdate,omitempty"`
}

// Validate enforces the exactly-one rule for StreamResponse.
func (r StreamResponse) Validate() error {
	n := 0
	for _, set := range []bool{r.Task != nil, r.Message != nil, r.StatusUpdate != nil, r.ArtifactUpdate != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("a2a: stream response must carry exactly one of task, message, statusUpdate, artifactUpdate; got %d", n)
	}
	return nil
}

// SendMessageResponse is the non-streaming result: a task (the usual
// outcome) or, for an agent that answers without a task, a message.
type SendMessageResponse struct {
	Task    *Task    `json:"task,omitempty"`
	Message *Message `json:"message,omitempty"`
}

// SendMessageConfiguration tunes one SendMessage call.
type SendMessageConfiguration struct {
	AcceptedOutputModes        []string                `json:"acceptedOutputModes,omitempty"`
	TaskPushNotificationConfig *PushNotificationConfig `json:"taskPushNotificationConfig,omitempty"`
	HistoryLength              *int                    `json:"historyLength,omitempty"`
	ReturnImmediately          bool                    `json:"returnImmediately,omitempty"`
}

// SendMessageRequest is the params object of SendMessage and
// SendStreamingMessage.
type SendMessageRequest struct {
	Tenant        string                    `json:"tenant,omitempty"`
	Message       *Message                  `json:"message"`
	Configuration *SendMessageConfiguration `json:"configuration,omitempty"`
	Metadata      map[string]any            `json:"metadata,omitempty"`
}

// GetTaskRequest is the params object of GetTask.
type GetTaskRequest struct {
	Tenant        string `json:"tenant,omitempty"`
	ID            string `json:"id"`
	HistoryLength *int   `json:"historyLength,omitempty"`
}

// ListTasksRequest is the params object of ListTasks.
type ListTasksRequest struct {
	Tenant               string     `json:"tenant,omitempty"`
	ContextID            string     `json:"contextId,omitempty"`
	Status               TaskState  `json:"status,omitempty"`
	PageSize             *int       `json:"pageSize,omitempty"`
	PageToken            string     `json:"pageToken,omitempty"`
	HistoryLength        *int       `json:"historyLength,omitempty"`
	StatusTimestampAfter *Timestamp `json:"statusTimestampAfter,omitempty"`
	IncludeArtifacts     *bool      `json:"includeArtifacts,omitempty"`
}

// ListTasksResponse is the result of ListTasks. Tasks is never null on the
// wire: an empty page encodes as [].
type ListTasksResponse struct {
	Tasks         []Task `json:"tasks"`
	NextPageToken string `json:"nextPageToken"`
	PageSize      int    `json:"pageSize"`
	TotalSize     int    `json:"totalSize"`
}

// CancelTaskRequest is the params object of CancelTask.
type CancelTaskRequest struct {
	Tenant   string         `json:"tenant,omitempty"`
	ID       string         `json:"id"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SubscribeToTaskRequest is the params object of SubscribeToTask.
type SubscribeToTaskRequest struct {
	Tenant string `json:"tenant,omitempty"`
	ID     string `json:"id"`
}

// AuthenticationInfo tells the agent how to authenticate to a push URL.
// Scheme "Bearer" or "Basic" become an Authorization header built from
// Credentials verbatim; other schemes are stored and ignored on send.
type AuthenticationInfo struct {
	Scheme      string `json:"scheme"`
	Credentials string `json:"credentials,omitempty"`
}

// PushNotificationConfig is a webhook the agent notifies about one task.
// It is both the params object of CreateTaskPushNotificationConfig and the
// result of the create/get calls.
type PushNotificationConfig struct {
	Tenant         string              `json:"tenant,omitempty"`
	ID             string              `json:"id,omitempty"`
	TaskID         string              `json:"taskId"`
	URL            string              `json:"url"`
	Token          string              `json:"token,omitempty"`
	Authentication *AuthenticationInfo `json:"authentication,omitempty"`
}

// GetTaskPushNotificationConfigRequest is the params object of
// GetTaskPushNotificationConfig.
type GetTaskPushNotificationConfigRequest struct {
	Tenant string `json:"tenant,omitempty"`
	TaskID string `json:"taskId"`
	ID     string `json:"id"`
}

// ListTaskPushNotificationConfigsRequest is the params object of
// ListTaskPushNotificationConfigs.
type ListTaskPushNotificationConfigsRequest struct {
	Tenant    string `json:"tenant,omitempty"`
	TaskID    string `json:"taskId"`
	PageSize  *int   `json:"pageSize,omitempty"`
	PageToken string `json:"pageToken,omitempty"`
}

// ListTaskPushNotificationConfigsResponse is the result of
// ListTaskPushNotificationConfigs. Configs is never null on the wire.
type ListTaskPushNotificationConfigsResponse struct {
	Configs       []PushNotificationConfig `json:"configs"`
	NextPageToken string                   `json:"nextPageToken,omitempty"`
}

// DeleteTaskPushNotificationConfigRequest is the params object of
// DeleteTaskPushNotificationConfig. The result is an empty object.
type DeleteTaskPushNotificationConfigRequest struct {
	Tenant string `json:"tenant,omitempty"`
	TaskID string `json:"taskId"`
	ID     string `json:"id"`
}

// GetExtendedAgentCardRequest is the params object of GetExtendedAgentCard.
type GetExtendedAgentCardRequest struct {
	Tenant string `json:"tenant,omitempty"`
}

// PushNotificationTokenHeader is the header carrying
// PushNotificationConfig.Token on a push delivery, so the receiver can
// check the notification came from the agent it registered with.
const PushNotificationTokenHeader = "A2A-Notification-Token" // not-a-secret: a header name
