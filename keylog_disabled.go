//go:build !dgproto_keylog

package dgproto

func keylogWrite([16]byte, [KeySize]byte, [KeySize]byte) {}

func keylogWriteEpoch([16]byte, string, uint32, [KeySize]byte) {}
