package api

import (
	"fmt"

	"proxyma/internal/ids"
)

func requireUUIDV4(value, field string) error {
	if !ids.IsUUIDV4(value) {
		return validationError(fmt.Sprintf("%s must be a UUID v4", field))
	}
	return nil
}

func requireOptionalUUIDV4(value, field string) error {
	if value == "" {
		return nil
	}
	return requireUUIDV4(value, field)
}

func requireUUIDV4List(values []string, field string) error {
	for index, value := range values {
		if err := requireUUIDV4(value, fmt.Sprintf("%s[%d]", field, index)); err != nil {
			return err
		}
	}
	return nil
}
