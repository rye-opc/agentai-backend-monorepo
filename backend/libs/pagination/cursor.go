package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type TimeIDCursor struct {
	T  int64  `json:"t"`  // unix millis
	ID string `json:"id"` // stable tie-breaker (e.g. uuid)
}

func EncodeTimeIDCursor(c TimeIDCursor) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshal cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func DecodeTimeIDCursor(token string) (TimeIDCursor, error) {
	if token == "" {
		return TimeIDCursor{}, fmt.Errorf("empty page token")
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return TimeIDCursor{}, fmt.Errorf("decode page token: %w", err)
	}
	var c TimeIDCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return TimeIDCursor{}, fmt.Errorf("unmarshal cursor: %w", err)
	}
	return c, nil
}
