package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonIterations = uint32(2)
	argonMemoryKiB  = uint32(19 * 1024)
	argonParallel   = uint8(1)
	argonSaltLength = 16
	argonKeyLength  = uint32(32)
	maxArgonMemory  = uint32(256 * 1024)
	maxArgonTime    = uint32(10)
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, argonParallel, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemoryKiB, argonIterations, argonParallel,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	params := make(map[string]uint32, 3)
	for _, item := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return false
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return false
		}
		params[key] = uint32(parsed)
	}
	memory, hasMemory := params["m"]
	iterations, hasIterations := params["t"]
	parallelism, hasParallelism := params["p"]
	if !hasMemory || !hasIterations || !hasParallelism || len(params) != 3 ||
		memory < 8*parallelism || memory > maxArgonMemory || iterations == 0 || iterations > maxArgonTime || parallelism == 0 || parallelism > 8 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, uint8(parallelism), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func validatePassword(password string) error {
	length := len([]byte(password))
	if length < 12 || length > 1024 {
		return ErrWeakPassword
	}
	return nil
}

// ValidatePassword enforces the bootstrap and password-change policy.
func ValidatePassword(password string) error { return validatePassword(password) }
