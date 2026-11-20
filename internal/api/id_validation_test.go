package api

import (
	"net/http"
	"testing"
)

func TestRequireUUIDV4(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "canonical lowercase v4", value: "fcd2f3a9-d546-44f4-837b-0c7aa4219752", valid: true},
		{name: "empty", value: "", valid: false},
		{name: "malformed", value: "not-a-uuid", valid: false},
		{name: "v1", value: "6ba7b810-9dad-11d1-80b4-00c04fd430c8", valid: false},
		{name: "uppercase", value: "FCD2F3A9-D546-44F4-837B-0C7AA4219752", valid: false},
		{name: "traversal-like", value: "../fcd2f3a9-d546-44f4-837b-0c7aa4219752", valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := requireUUIDV4(test.value, "resource_id")
			if test.valid && err != nil {
				t.Fatalf("requireUUIDV4(%q) error = %v", test.value, err)
			}
			if !test.valid {
				if err == nil {
					t.Fatalf("requireUUIDV4(%q) unexpectedly succeeded", test.value)
				}
				status, payload := errorResponse(err)
				if status != http.StatusBadRequest || payload.Error == "" {
					t.Fatalf("error response = (%d, %#v), want bad request", status, payload)
				}
			}
		})
	}
}

func TestRequireUUIDV4List(t *testing.T) {
	if err := requireUUIDV4List([]string{"fcd2f3a9-d546-44f4-837b-0c7aa4219752", "df9ec8bb-1a22-4181-a7c8-126ad553221d"}, "ids"); err != nil {
		t.Fatalf("valid UUID list rejected: %v", err)
	}
	if err := requireUUIDV4List([]string{"fcd2f3a9-d546-44f4-837b-0c7aa4219752", "../bad"}, "ids"); err == nil {
		t.Fatal("malformed UUID list unexpectedly accepted")
	}
}
