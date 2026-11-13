package custody

import (
	"encoding/json"
	"testing"
	"time"
)

func buildChain(n int) []Event {
	prev := GenesisHash
	var out []Event
	for i := 1; i <= n; i++ {
		e := Event{Seq: int64(i), ID: "id", Action: "test", Actor: "tester",
			Details: json.RawMessage(`{"i":1}`), OccurredAt: time.Now(), PrevHash: prev}
		e.Hash = ComputeHash(e)
		prev = e.Hash
		out = append(out, e)
	}
	return out
}

func TestChainValid(t *testing.T) {
	v := NewVerifier()
	for _, e := range buildChain(5) {
		v.Add(e)
	}
	if r := v.Result(); !r.Valid || r.Checked != 5 {
		t.Fatalf("expected valid chain, got %+v", r)
	}
}

func TestChainTampered(t *testing.T) {
	chain := buildChain(5)
	chain[2].Actor = "someone else"
	v := NewVerifier()
	for _, e := range chain {
		v.Add(e)
	}
	if r := v.Result(); r.Valid || r.BrokenAt != 3 {
		t.Fatalf("expected break at 3, got %+v", r)
	}
}

func TestChainDeleted(t *testing.T) {
	chain := buildChain(5)
	chain = append(chain[:1], chain[2:]...)
	v := NewVerifier()
	for _, e := range chain {
		v.Add(e)
	}
	if r := v.Result(); r.Valid {
		t.Fatal("expected deletion to be detected")
	}
}
