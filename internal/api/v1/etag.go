package v1

import (
	"strconv"
	"strings"
)

func parseIfMatch(value string) (int64, error) {
	if len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
		return 0, ErrInvalidRequest
	}
	raw := value[1 : len(value)-1]
	if raw == "" || strings.ContainsAny(raw, "\",* \t\r\n") || raw[0] == '0' {
		return 0, ErrInvalidRequest
	}
	version, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || version <= 0 {
		return 0, ErrInvalidRequest
	}
	return version, nil
}

func formatETag(version int64) string {
	return `"` + strconv.FormatInt(version, 10) + `"`
}
