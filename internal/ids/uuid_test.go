package ids_test

import (
	"strings"
	"testing"

	"github.com/projecthub/projecthub/internal/ids"
)

func TestNewUUIDReturnsUniqueRFC4122Version4Identifiers(t *testing.T) {
	first, err := ids.NewUUID()
	if err != nil {
		t.Fatalf("NewUUID() error = %v", err)
	}
	second, err := ids.NewUUID()
	if err != nil {
		t.Fatalf("second NewUUID() error = %v", err)
	}
	if first == second || !ids.IsUUID(first) || !ids.IsUUID(second) {
		t.Fatalf("generated IDs are invalid or repeated: %q, %q", first, second)
	}
	if first[14] != '4' || !strings.ContainsRune("89ab", rune(strings.ToLower(first)[19])) {
		t.Fatalf("UUID %q does not have version 4 and RFC4122 variant bits", first)
	}
}

func TestIsUUIDRejectsMalformedIdentifiers(t *testing.T) {
	for _, value := range []string{"", "not-a-uuid", "00000000000000000000000000000000", "gggggggg-gggg-4ggg-8ggg-gggggggggggg"} {
		if ids.IsUUID(value) {
			t.Errorf("IsUUID(%q) = true, want false", value)
		}
	}
}
