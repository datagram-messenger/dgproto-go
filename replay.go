package dgproto

import (
	"errors"

	"github.com/datagram-messenger/dgproto-go/internal/replay"
)

const (
	// ReplayWindowSize is the number of recent sequence numbers tracked.
	ReplayWindowSize = 2048
	replayWordCount  = ReplayWindowSize / 64
)

var (
	// ErrReplayZero indicates that sequence zero was presented to the replay window.
	ErrReplayZero = errors.New("dgproto: sequence number is zero")
	// ErrReplayDuplicate indicates that a sequence is already recorded.
	ErrReplayDuplicate = errors.New("dgproto: duplicate sequence number")
	// ErrReplayTooOld indicates that a sequence precedes the tracked replay window.
	ErrReplayTooOld = errors.New("dgproto: sequence number is outside replay window")
	// ErrReplayStale indicates that a ReplayToken was invalidated by another commit.
	ErrReplayStale = errors.New("dgproto: stale replay commit token")
)

// ReplayWindow tracks authenticated receive sequences. It is session-owned
// state and is not safe for concurrent use.
type ReplayWindow struct {
	highest    uint64
	bitmap     [replayWordCount]uint64
	generation uint64
}

// ReplayToken records a successful Check for later authenticated Commit.
type ReplayToken struct {
	sequence   uint64
	generation uint64
}

// Check validates sequence without changing the replay window.
func (w *ReplayWindow) Check(sequence uint64) (ReplayToken, error) {
	if sequence == 0 {
		return ReplayToken{}, ErrReplayZero
	}
	if err := replay.Check(w.highest, &w.bitmap, sequence); err != nil {
		return ReplayToken{}, replayError(err)
	}
	return ReplayToken{sequence: sequence, generation: w.generation}, nil
}

// Commit records a sequence after its packet has been authenticated. A token
// becomes stale after any successful Commit and must then be checked again.
func (w *ReplayWindow) Commit(token ReplayToken) error {
	if token.sequence == 0 {
		return ErrReplayStale
	}
	return replayError(replay.Commit(&w.highest, &w.bitmap, &w.generation, token.sequence, token.generation))
}

func (w *ReplayWindow) marked(distance uint64) bool {
	return replay.Marked(&w.bitmap, distance)
}

func (w *ReplayWindow) set(distance uint64) {
	replay.Set(&w.bitmap, distance)
}

func (w *ReplayWindow) advance(shift uint64) {
	replay.Advance(&w.bitmap, shift)
}

// Keep public sentinels root-owned, including their existing variable semantics.
func replayError(err error) error {
	switch err {
	case replay.ErrZero:
		return ErrReplayZero
	case replay.ErrDuplicate:
		return ErrReplayDuplicate
	case replay.ErrTooOld:
		return ErrReplayTooOld
	case replay.ErrStale:
		return ErrReplayStale
	default:
		return err
	}
}
