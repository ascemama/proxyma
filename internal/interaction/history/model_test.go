package history

import (
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	created := time.Date(2026, 11, 4, 1, 2, 3, 4, time.UTC)
	encoded := EncodeCursor(created, "command-1")
	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.CreatedAt.Equal(created) || decoded.HistoryID != "command-1" {
		t.Fatalf("decoded cursor = %#v", decoded)
	}
}

func TestNormalizeLimit(t *testing.T) {
	if got := NormalizeLimit(0); got != DefaultLimit {
		t.Fatalf("default limit = %d", got)
	}
	if got := NormalizeLimit(MaxLimit + 1); got != MaxLimit {
		t.Fatalf("max limit = %d", got)
	}
}
