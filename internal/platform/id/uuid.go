package id

import (
	"crypto/rand"
	"fmt"
)

type Generator func() (string, error)

// UUID returns a random RFC 4122 version 4 identifier using only the standard
// library, keeping identifier generation independent from storage packages.
func UUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16],
	), nil
}
