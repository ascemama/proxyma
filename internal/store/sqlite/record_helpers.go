package sqlite

import (
	"database/sql"
	"time"
)

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func scanTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseRequiredTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func stringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func boolPointer(value sql.NullInt64) *bool {
	if !value.Valid {
		return nil
	}
	result := value.Int64 != 0
	return &result
}

func boolValue(value *bool) any {
	if value == nil {
		return nil
	}
	if *value {
		return int64(1)
	}
	return int64(0)
}
