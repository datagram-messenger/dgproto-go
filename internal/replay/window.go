// Package replay implements the fixed-size authenticated sequence window.
// Callers own storage and synchronization; Check never mutates that storage.
package replay

import "errors"

const (
	WindowSize = 2048
	WordCount  = WindowSize / 64
)

var (
	ErrZero      = errors.New("dgproto: sequence number is zero")
	ErrDuplicate = errors.New("dgproto: duplicate sequence number")
	ErrTooOld    = errors.New("dgproto: sequence number is outside replay window")
	ErrStale     = errors.New("dgproto: stale replay commit token")
)

// Check validates a sequence without recording it. A caller captures its
// generation with the sequence for a later authenticated Commit.
func Check(highest uint64, bitmap *[WordCount]uint64, sequence uint64) error {
	if sequence == 0 {
		return ErrZero
	}
	if sequence <= highest {
		distance := highest - sequence
		if distance >= WindowSize {
			return ErrTooOld
		}
		if Marked(bitmap, distance) {
			return ErrDuplicate
		}
	}
	return nil
}

// Commit records an authenticated sequence only if its captured generation is
// current. Rejection leaves all caller-owned state unchanged. Pointer arguments
// must refer to one window and must not alias each other.
func Commit(highest *uint64, bitmap *[WordCount]uint64, generation *uint64, sequence, tokenGeneration uint64) error {
	if sequence == 0 || tokenGeneration != *generation {
		return ErrStale
	}

	if sequence <= *highest {
		distance := *highest - sequence
		if distance >= WindowSize || Marked(bitmap, distance) {
			return ErrStale
		}
		Set(bitmap, distance)
	} else {
		Advance(bitmap, sequence-*highest)
		*highest = sequence
		Set(bitmap, 0)
	}
	*generation++
	return nil
}

// Marked reports whether distance is recorded; distance must be < WindowSize.
func Marked(bitmap *[WordCount]uint64, distance uint64) bool {
	return bitmap[distance/64]&(uint64(1)<<(distance%64)) != 0
}

// Set records distance; distance must be < WindowSize.
func Set(bitmap *[WordCount]uint64, distance uint64) {
	bitmap[distance/64] |= uint64(1) << (distance % 64)
}

// Advance shifts the window toward a higher sequence, discarding old bits.
func Advance(bitmap *[WordCount]uint64, shift uint64) {
	if shift >= WindowSize {
		clear(bitmap[:])
		return
	}

	wordShift := int(shift / 64)
	bitShift := uint(shift % 64)
	for dst := WordCount - 1; dst >= 0; dst-- {
		src := dst - wordShift
		var value uint64
		if src >= 0 {
			value = bitmap[src] << bitShift
			if bitShift != 0 && src > 0 {
				value |= bitmap[src-1] >> (64 - bitShift)
			}
		}
		bitmap[dst] = value
	}
}
