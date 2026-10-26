package dgproto_test

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

var _ interface {
	Check(uint64) (dgproto.ReplayToken, error)
	Commit(dgproto.ReplayToken) error
} = (*dgproto.ReplayWindow)(nil)

func TestReplayPublicIdentity(t *testing.T) {
	for _, tc := range []struct {
		typ  reflect.Type
		name string
	}{
		{reflect.TypeOf(dgproto.ReplayWindow{}), "ReplayWindow"},
		{reflect.TypeOf(dgproto.ReplayToken{}), "ReplayToken"},
	} {
		// Concrete identity and comparability are consumer-visible; private
		// field names, field counts and byte sizes are not a storage contract.
		if tc.typ.PkgPath() != "github.com/datagram-messenger/dgproto-go" || tc.typ.Name() != tc.name || !tc.typ.Comparable() {
			t.Fatalf("public identity changed: %v", tc.typ)
		}
	}
}

func TestReplayPublicErrorsAndValueOwnership(t *testing.T) {
	var w dgproto.ReplayWindow
	before := w
	token, err := w.Check(3000)
	if err != nil || w != before {
		t.Fatalf("Check mutated state or failed: %v", err)
	}
	copyWindow := w
	if err := copyWindow.Commit(token); err != nil || w != before {
		t.Fatalf("commit to value copy affected original: %v", err)
	}
	if err := w.Commit(token); err != nil {
		t.Fatal(err)
	}
	before = w
	for _, tc := range []struct {
		sequence uint64
		want     error
	}{
		{0, dgproto.ErrReplayZero}, {3000, dgproto.ErrReplayDuplicate}, {952, dgproto.ErrReplayTooOld},
	} {
		gotToken, err := w.Check(tc.sequence)
		if err != tc.want || !errors.Is(fmt.Errorf("outer: %w", err), tc.want) || gotToken != (dgproto.ReplayToken{}) || w != before {
			t.Fatalf("Check(%d) = %v; identity, zero token or state changed", tc.sequence, err)
		}
	}
	for _, stale := range []dgproto.ReplayToken{token, {}} {
		if err := w.Commit(stale); err != dgproto.ErrReplayStale || w != before {
			t.Fatalf("rejected Commit changed state or sentinel: %v", err)
		}
	}
	token, err = w.Check(math.MaxUint64)
	if err != nil || w.Commit(token) != nil {
		t.Fatal("maximum sequence failed")
	}
}

func TestReplayPublicSentinelVariableSemantics(t *testing.T) {
	original := dgproto.ErrReplayDuplicate
	replacement := &replayTestError{}
	dgproto.ErrReplayDuplicate = replacement
	defer func() { dgproto.ErrReplayDuplicate = original }()
	var w dgproto.ReplayWindow
	token, err := w.Check(1)
	if err != nil || w.Commit(token) != nil {
		t.Fatal("setup failed")
	}
	_, err = w.Check(1)
	var typed *replayTestError
	if err != replacement || !errors.As(fmt.Errorf("outer: %w", err), &typed) || typed != replacement {
		t.Fatal("public sentinel variable or errors.As behavior changed")
	}
}

type replayTestError struct{}

func (*replayTestError) Error() string { return "synthetic replay error" }

func TestReplayPublicNilReceiverCompatibility(t *testing.T) {
	var w *dgproto.ReplayWindow
	if _, err := w.Check(0); err != dgproto.ErrReplayZero {
		t.Fatal(err)
	}
	if err := w.Commit(dgproto.ReplayToken{}); err != dgproto.ErrReplayStale {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Error("nonzero Check on nil receiver no longer panics")
		}
	}()
	_, _ = w.Check(1)
}
