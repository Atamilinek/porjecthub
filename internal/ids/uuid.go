package ids

import (
	"crypto/rand"
	"encoding/hex"
)

// NewUUID returns a randomly generated RFC 4122 version 4 UUID.
func NewUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	var encoded [32]byte
	hex.Encode(encoded[:], raw[:])
	return string(encoded[0:8]) + "-" + string(encoded[8:12]) + "-" + string(encoded[12:16]) + "-" + string(encoded[16:20]) + "-" + string(encoded[20:32]), nil
}

// IsUUID reports whether value is a canonical hyphenated UUID.
func IsUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	var compact [32]byte
	index := 0
	for i := range value {
		if value[i] == '-' {
			continue
		}
		compact[index] = value[i]
		index++
	}
	var decoded [16]byte
	_, err := hex.Decode(decoded[:], compact[:])
	return err == nil && index == len(compact) && decoded[6]>>4 >= 1 && decoded[6]>>4 <= 8 && decoded[8]&0xc0 == 0x80
}
