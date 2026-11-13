// Package custody implements Lumen's tamper-evident chain of custody.
//
// Every custody event is linked to the previous one by including the previous
// event's hash in its own hash. Altering, removing or reordering any event
// breaks the chain from that point on, which VerifyChain detects.
package custody

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"strconv"
	"time"
)

// GenesisHash is the prev_hash of the very first event in a chain.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// TimeLayout is the canonical timestamp format used inside the hash.
// Timestamps are truncated to microseconds (Postgres precision) and UTC.
const TimeLayout = "2006-01-02T15:04:05.000000Z"

// Event is a single, immutable custody record.
type Event struct {
	Seq        int64           `json:"seq"`
	ID         string          `json:"id"`
	CaseID     *string         `json:"case_id"`
	EvidenceID *string         `json:"evidence_id"`
	Action     string          `json:"action"`
	Actor      string          `json:"actor"`
	Details    json.RawMessage `json:"details"`
	OccurredAt time.Time       `json:"occurred_at"`
	PrevHash   string          `json:"prev_hash"`
	Hash       string          `json:"hash"`
}

// CanonicalTime normalises a time to the precision stored and hashed.
func CanonicalTime(t time.Time) time.Time {
	return t.UTC().Truncate(time.Microsecond)
}

func writeField(h hash.Hash, s string) {
	// Length-prefixed fields make the encoding unambiguous.
	fmt.Fprintf(h, "%d:%s\n", len(s), s)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ComputeHash returns the SHA-256 hash of an event's canonical encoding.
// The Hash field itself is not part of the input.
func ComputeHash(e Event) string {
	h := sha256.New()
	writeField(h, "lumen-custody-v1")
	writeField(h, strconv.FormatInt(e.Seq, 10))
	writeField(h, e.ID)
	writeField(h, deref(e.CaseID))
	writeField(h, deref(e.EvidenceID))
	writeField(h, e.Action)
	writeField(h, e.Actor)
	writeField(h, string(e.Details))
	writeField(h, CanonicalTime(e.OccurredAt).Format(TimeLayout))
	writeField(h, e.PrevHash)
	return hex.EncodeToString(h.Sum(nil))
}

// Result describes the outcome of a chain verification.
type Result struct {
	Valid      bool   `json:"valid"`
	Checked    int    `json:"checked"`
	BrokenAt   int64  `json:"broken_at,omitempty"`
	Reason     string `json:"reason,omitempty"`
	HeadSeq    int64  `json:"head_seq"`
	HeadHash   string `json:"head_hash"`
	VerifiedAt string `json:"verified_at"`
}

// Verifier checks a full chain incrementally, in sequence order.
type Verifier struct {
	res      Result
	prevSeq  int64
	prevHash string
	failed   bool
}

func NewVerifier() *Verifier {
	return &Verifier{prevHash: GenesisHash, res: Result{Valid: true, HeadHash: GenesisHash}}
}

// Add checks the next event. It returns false once the chain is broken.
func (v *Verifier) Add(e Event) bool {
	if v.failed {
		return false
	}
	fail := func(reason string) bool {
		v.failed = true
		v.res.Valid = false
		v.res.BrokenAt = e.Seq
		v.res.Reason = reason
		return false
	}
	v.res.Checked++
	if e.Seq != v.prevSeq+1 {
		return fail(fmt.Sprintf("sequence gap: expected %d, found %d", v.prevSeq+1, e.Seq))
	}
	if e.PrevHash != v.prevHash {
		return fail("prev_hash does not match the hash of the preceding event")
	}
	if got := ComputeHash(e); got != e.Hash {
		return fail("event content does not match its recorded hash")
	}
	v.prevSeq, v.prevHash = e.Seq, e.Hash
	v.res.HeadSeq, v.res.HeadHash = e.Seq, e.Hash
	return true
}

func (v *Verifier) Result() Result {
	r := v.res
	r.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
	return r
}

// VerifySelf checks only that an event's content matches its own hash.
// Used for exported subsets of the chain where neighbours are absent.
func VerifySelf(e Event) bool {
	return ComputeHash(e) == e.Hash
}
