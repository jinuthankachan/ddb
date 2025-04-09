package pbft

import (
	"sync"
)

// PBFTState represents the consensus state of a PBFT node
type PBFTState struct {
	View           uint64
	NextSequence   uint64
	LowWatermark   uint64 // h: lowest sequence number still in log
	HighWatermark  uint64 // H: highest acceptable sequence number
	FaultTolerance int    // f: maximum number of faulty nodes

	// Log storage
	prePrepareLog map[viewSeq]*PrePrepareMessage
	prepareLog    map[viewSeq]map[string]*PrepareMessage // nodeID -> PrepareMessage
	commitLog     map[viewSeq]map[string]*CommitMessage  // nodeID -> CommitMessage

	// Prepared and committed flags
	prepared  map[viewSeqDigest]bool
	committed map[viewSeqDigest]bool

	requests map[viewSeq]*RequestMessage

	mu sync.RWMutex
}

// viewSeq is a composite key for view and sequence number
type viewSeq struct {
	View     uint64
	Sequence uint64
}

// viewSeqDigest is a composite key for view, sequence number, and request digest
type viewSeqDigest struct {
	View     uint64
	Sequence uint64
	Digest   string
}

// NewPBFTState creates a new PBFT state
func NewPBFTState(faultTolerance int) *PBFTState {
	return &PBFTState{
		View:           0,
		NextSequence:   1,
		LowWatermark:   0,
		HighWatermark:  1000, // Initial window size, adjust as needed
		FaultTolerance: faultTolerance,

		prePrepareLog: make(map[viewSeq]*PrePrepareMessage),
		prepareLog:    make(map[viewSeq]map[string]*PrepareMessage),
		commitLog:     make(map[viewSeq]map[string]*CommitMessage),

		prepared:  make(map[viewSeqDigest]bool),
		committed: make(map[viewSeqDigest]bool),

		requests: make(map[viewSeq]*RequestMessage),
	}
}

// LogPrePrepare logs a PRE-PREPARE message
func (s *PBFTState) LogPrePrepare(msg *PrePrepareMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := viewSeq{View: msg.View, Sequence: msg.SequenceNumber}
	s.prePrepareLog[key] = msg

	// Store the original request if available
	if msg.Request != nil {
		s.requests[key] = msg.Request
	}
}

// LogPrepare logs a PREPARE message
func (s *PBFTState) LogPrepare(msg *PrepareMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := viewSeq{View: msg.View, Sequence: msg.SequenceNumber}

	// Initialize the prepare log for this view and sequence if needed
	if _, exists := s.prepareLog[key]; !exists {
		s.prepareLog[key] = make(map[string]*PrepareMessage)
	}

	// Log the prepare message
	s.prepareLog[key][msg.SenderID] = msg

	// Check if we've reached the prepared state
	if s.prepareQuorumReached(key, msg.RequestDigest) {
		digestKey := viewSeqDigest{
			View:     msg.View,
			Sequence: msg.SequenceNumber,
			Digest:   msg.RequestDigest,
		}
		s.prepared[digestKey] = true
	}
}

// LogCommit logs a COMMIT message
func (s *PBFTState) LogCommit(msg *CommitMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := viewSeq{View: msg.View, Sequence: msg.SequenceNumber}

	// Initialize the commit log for this view and sequence if needed
	if _, exists := s.commitLog[key]; !exists {
		s.commitLog[key] = make(map[string]*CommitMessage)
	}

	// Log the commit message
	s.commitLog[key][msg.SenderID] = msg

	// Check if we've reached the committed state
	if s.commitQuorumReached(key, msg.RequestDigest) {
		digestKey := viewSeqDigest{
			View:     msg.View,
			Sequence: msg.SequenceNumber,
			Digest:   msg.RequestDigest,
		}
		s.committed[digestKey] = true
	}
}

// HasPrePrepared checks if a PRE-PREPARE exists for the given view and sequence
func (s *PBFTState) HasPrePrepared(view, seq uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := viewSeq{View: view, Sequence: seq}
	_, exists := s.prePrepareLog[key]
	return exists
}

// HasPrepared checks if we have reached the prepared state
func (s *PBFTState) HasPrepared(view, seq uint64, digest string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	digestKey := viewSeqDigest{
		View:     view,
		Sequence: seq,
		Digest:   digest,
	}
	return s.prepared[digestKey]
}

// HasCommitted checks if we have reached the committed state
func (s *PBFTState) HasCommitted(view, seq uint64, digest string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	digestKey := viewSeqDigest{
		View:     view,
		Sequence: seq,
		Digest:   digest,
	}
	return s.committed[digestKey]
}

// GetRequest retrieves the original request for a given view and sequence
func (s *PBFTState) GetRequest(view, seq uint64) *RequestMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := viewSeq{View: view, Sequence: seq}
	return s.requests[key]
}

// prepareQuorumReached checks if we have enough PREPARE messages (2f+1 including our own)
func (s *PBFTState) prepareQuorumReached(key viewSeq, digest string) bool {
	// Make sure we have a PRE-PREPARE for this view/sequence
	prePrepare, exists := s.prePrepareLog[key]
	if !exists || prePrepare.RequestDigest != digest {
		return false
	}

	prepareCount := 0
	for _, prepare := range s.prepareLog[key] {
		if prepare.RequestDigest == digest {
			prepareCount++
		}
	}

	// Need 2f prepares (PRE-PREPARE counts as a prepare from the primary)
	return prepareCount >= 2*s.FaultTolerance
}

// commitQuorumReached checks if we have enough COMMIT messages (2f+1 including our own)
func (s *PBFTState) commitQuorumReached(key viewSeq, digest string) bool {
	commitCount := 0
	for _, commit := range s.commitLog[key] {
		if commit.RequestDigest == digest {
			commitCount++
		}
	}

	// Need 2f+1 commits
	return commitCount >= 2*s.FaultTolerance+1
}

// CleanupOldMessages removes messages below the low watermark (for garbage collection)
func (s *PBFTState) CleanupOldMessages(newLowWatermark uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update the watermark
	s.LowWatermark = newLowWatermark
	s.HighWatermark = newLowWatermark + 1000 // Simple fixed window size

	// Clean up logs
	for key := range s.prePrepareLog {
		if key.Sequence <= newLowWatermark {
			delete(s.prePrepareLog, key)
		}
	}

	for key := range s.prepareLog {
		if key.Sequence <= newLowWatermark {
			delete(s.prepareLog, key)
		}
	}

	for key := range s.commitLog {
		if key.Sequence <= newLowWatermark {
			delete(s.commitLog, key)
		}
	}

	for key := range s.requests {
		if key.Sequence <= newLowWatermark {
			delete(s.requests, key)
		}
	}

	// Clean up prepared/committed flags
	for key := range s.prepared {
		if key.Sequence <= newLowWatermark {
			delete(s.prepared, key)
		}
	}

	for key := range s.committed {
		if key.Sequence <= newLowWatermark {
			delete(s.committed, key)
		}
	}
}
