package jsonfile

import (
	"fmt"
	"strings"
	"time"
)

const recordTimeFormat = "20060102-150405"

func recordKey(createdAt time.Time, id string) string {
	id = strings.TrimSpace(id)
	if createdAt.IsZero() {
		return id
	}
	return fmt.Sprintf("%s-%s", createdAt.UTC().Format(recordTimeFormat), id)
}
