//go:build !dgproto_keylog

package dgproto

func keylogWrite([16]byte, [KeySize]byte, [KeySize]byte) {}
