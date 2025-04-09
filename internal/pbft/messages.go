package pbft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// MessageType defines the type of PBFT message
type MessageType string

const (
	TypeRequest    MessageType = "REQUEST"
	TypePrePrepare MessageType = "PRE-PREPARE"
	TypePrepare    MessageType = "PREPARE"
	TypeCommit     MessageType = "COMMIT"
	TypeViewChange MessageType = "VIEW-CHANGE"
	TypeNewView    MessageType = "NEW-VIEW"
	TypeCheckpoint MessageType = "CHECKPOINT"
	TypeReply      MessageType = "REPLY"
)

// Operation represents a key-value store operation
type Operation struct {
	Type  string `json:"type"` // "SET", "GET", "DELETE"
	Key   string `json:"key"`
	Value []byte `json:"value,omitempty"`
}

// BaseMessage contains fields common to all PBFT messages
type BaseMessage struct {
	Type           MessageType `json:"type"`
	View           uint64      `json:"view"`
	SequenceNumber uint64      `json:"sequence_number"`
	SenderID       string      `json:"sender_id"`
	Timestamp      int64       `json:"timestamp"`
	Signature      []byte      `json:"signature,omitempty"`
}

// RequestMessage represents a client request
type RequestMessage struct {
	BaseMessage
	Operation Operation `json:"operation"`
	ClientID  string    `json:"client_id"`
}

// PrePrepareMessage represents a PRE-PREPARE message from the primary
type PrePrepareMessage struct {
	BaseMessage
	RequestDigest string          `json:"request_digest"`
	Request       *RequestMessage `json:"request,omitempty"` // Full request might be included
}

// PrepareMessage represents a PREPARE message from a backup
type PrepareMessage struct {
	BaseMessage
	RequestDigest string `json:"request_digest"`
}

// CommitMessage represents a COMMIT message
type CommitMessage struct {
	BaseMessage
	RequestDigest string `json:"request_digest"`
}

// ReplyMessage represents a reply to a client request
type ReplyMessage struct {
	BaseMessage
	ClientID     string `json:"client_id"`
	Result       []byte `json:"result,omitempty"`
	ResultStatus string `json:"result_status"` // "SUCCESS", "ERROR", etc.
}

// CheckpointMessage represents a CHECKPOINT message for garbage collection
type CheckpointMessage struct {
	BaseMessage
	StateDigest string `json:"state_digest"` // Digest of KV store state at sequence number
}

// ComputeDigest calculates a SHA-256 digest of a request message
func ComputeDigest(request *RequestMessage) string {
	// We only hash the essential elements, not the whole message with sig/timestamp
	hashContent := struct {
		ClientID  string    `json:"client_id"`
		Operation Operation `json:"operation"`
		View      uint64    `json:"view"`
		Sequence  uint64    `json:"sequence"`
	}{
		ClientID:  request.ClientID,
		Operation: request.Operation,
		View:      request.View,
		Sequence:  request.SequenceNumber,
	}

	bytes, _ := json.Marshal(hashContent)
	hash := sha256.Sum256(bytes)
	return hex.EncodeToString(hash[:])
}

// NewRequestMessage creates a new client request message
func NewRequestMessage(clientID string, op Operation) *RequestMessage {
	return &RequestMessage{
		BaseMessage: BaseMessage{
			Type:      TypeRequest,
			Timestamp: time.Now().UnixNano(),
			SenderID:  clientID,
		},
		Operation: op,
		ClientID:  clientID,
	}
}

// NewPrePrepareMessage creates a new PRE-PREPARE message
func NewPrePrepareMessage(view, seq uint64, nodeID string, request *RequestMessage) *PrePrepareMessage {
	digest := ComputeDigest(request)

	return &PrePrepareMessage{
		BaseMessage: BaseMessage{
			Type:           TypePrePrepare,
			View:           view,
			SequenceNumber: seq,
			SenderID:       nodeID,
			Timestamp:      time.Now().UnixNano(),
		},
		RequestDigest: digest,
		Request:       request,
	}
}

// NewPrepareMessage creates a new PREPARE message
func NewPrepareMessage(view, seq uint64, nodeID, digest string) *PrepareMessage {
	return &PrepareMessage{
		BaseMessage: BaseMessage{
			Type:           TypePrepare,
			View:           view,
			SequenceNumber: seq,
			SenderID:       nodeID,
			Timestamp:      time.Now().UnixNano(),
		},
		RequestDigest: digest,
	}
}

// NewCommitMessage creates a new COMMIT message
func NewCommitMessage(view, seq uint64, nodeID, digest string) *CommitMessage {
	return &CommitMessage{
		BaseMessage: BaseMessage{
			Type:           TypeCommit,
			View:           view,
			SequenceNumber: seq,
			SenderID:       nodeID,
			Timestamp:      time.Now().UnixNano(),
		},
		RequestDigest: digest,
	}
}

// NewReplyMessage creates a new REPLY message
func NewReplyMessage(view uint64, nodeID, clientID string, result []byte, status string) *ReplyMessage {
	return &ReplyMessage{
		BaseMessage: BaseMessage{
			Type:      TypeReply,
			View:      view,
			SenderID:  nodeID,
			Timestamp: time.Now().UnixNano(),
		},
		ClientID:     clientID,
		Result:       result,
		ResultStatus: status,
	}
}
