package storage

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const maxPageToken = 16384

type PageRequest struct {
	Signal       string
	Service      string
	Since, Until time.Time
	Limit        int
	Cursor       string
}

type PageResult struct {
	Signal     string    `json:"signal"`
	Since      time.Time `json:"since"`
	Until      time.Time `json:"until"`
	Records    any       `json:"records"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type pageToken struct {
	Version   int       `json:"version"`
	Database  string    `json:"database"`
	Signal    string    `json:"signal"`
	Service   string    `json:"service"`
	Since     time.Time `json:"since"`
	Until     time.Time `json:"until"`
	Limit     int       `json:"limit"`
	HighWater int64     `json:"high_water"`
	Count     int64     `json:"count"`
	LastTime  time.Time `json:"last_time"`
	LastID    int64     `json:"last_id"`
}

type pagePosition struct {
	highWater int64
	lastTime  time.Time
	lastID    int64
}

func (p pagePosition) appendWhere(query *string, timeColumn string) []any {
	*query += " AND cursor_id <= ?"
	args := []any{p.highWater}
	if p.lastID != 0 {
		*query += fmt.Sprintf(" AND (%s < ? OR (%s = ? AND cursor_id < ?))", timeColumn, timeColumn)
		args = append(args, p.lastTime, p.lastTime, p.lastID)
	}
	return args
}

func cursorMetadata(tx *sql.Tx, create bool) (string, []byte, error) {
	var identity, keyText string
	errID := tx.QueryRow(`SELECT value FROM schema_meta WHERE key='cursor_database_id'`).Scan(&identity)
	errKey := tx.QueryRow(`SELECT value FROM schema_meta WHERE key='cursor_hmac_key'`).Scan(&keyText)
	if errID == sql.ErrNoRows && errKey == sql.ErrNoRows && create {
		var existing int
		if err := tx.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name IN ('traces','logs') AND column_name='cursor_id'`).Scan(&existing); err != nil {
			return "", nil, err
		}
		if existing != 0 {
			return "", nil, fmt.Errorf("incomplete cursor capability")
		}
		for _, stmt := range []string{
			`CREATE SEQUENCE lotel_trace_cursor_seq START 1`,
			`CREATE SEQUENCE lotel_log_cursor_seq START 1`,
			`ALTER TABLE traces ADD COLUMN cursor_id BIGINT DEFAULT nextval('lotel_trace_cursor_seq')`,
			`ALTER TABLE logs ADD COLUMN cursor_id BIGINT DEFAULT nextval('lotel_log_cursor_seq')`,
			`ALTER TABLE traces ALTER COLUMN cursor_id SET NOT NULL`,
			`ALTER TABLE logs ALTER COLUMN cursor_id SET NOT NULL`,
			`CREATE UNIQUE INDEX lotel_trace_cursor_idx ON traces(cursor_id)`,
			`CREATE UNIQUE INDEX lotel_log_cursor_idx ON logs(cursor_id)`,
		} {
			if _, err := tx.Exec(stmt); err != nil {
				return "", nil, fmt.Errorf("creating cursor capability: %w", err)
			}
		}
		id := make([]byte, 16)
		key := make([]byte, 32)
		if _, err := rand.Read(id); err != nil {
			return "", nil, err
		}
		if _, err := rand.Read(key); err != nil {
			return "", nil, err
		}
		identity, keyText = hex.EncodeToString(id), hex.EncodeToString(key)
		if _, err := tx.Exec(`INSERT INTO schema_meta VALUES ('cursor_database_id', ?), ('cursor_hmac_key', ?)`, identity, keyText); err != nil {
			return "", nil, err
		}
	} else if errID != nil || errKey != nil {
		return "", nil, fmt.Errorf("cursor capability unavailable")
	}
	id, err := hex.DecodeString(identity)
	if err != nil || len(id) != 16 {
		return "", nil, fmt.Errorf("invalid cursor identity")
	}
	key, err := hex.DecodeString(keyText)
	if err != nil || len(key) != 32 {
		return "", nil, fmt.Errorf("invalid cursor key")
	}
	return identity, key, nil
}

func encodePageToken(token pageToken, key []byte) (string, error) {
	data, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decodePageToken(text string, key []byte) (pageToken, error) {
	var token pageToken
	if len(text) == 0 || len(text) > maxPageToken {
		return token, fmt.Errorf("invalid cursor")
	}
	parts := strings.Split(text, ".")
	if len(parts) != 2 {
		return token, fmt.Errorf("invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return token, fmt.Errorf("invalid cursor")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return token, fmt.Errorf("invalid cursor")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return token, fmt.Errorf("invalid cursor")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&token); err != nil {
		return token, fmt.Errorf("invalid cursor")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return token, fmt.Errorf("invalid cursor")
	}
	if token.Version != 1 || (token.Signal != "traces" && token.Signal != "logs") || token.Since.IsZero() || token.Until.IsZero() || token.Since.After(token.Until) || token.Limit < 1 || token.Limit > MaxRecentLimit || token.HighWater < 1 || token.Count < 1 || token.LastID < 1 || token.LastID > token.HighWater {
		return token, fmt.Errorf("invalid cursor")
	}
	return token, nil
}

func QueryPage(db *sql.DB, request PageRequest) (PageResult, error) {
	var result PageResult
	if request.Cursor == "" && ((request.Signal != "traces" && request.Signal != "logs") || request.Since.IsZero() || request.Until.IsZero() || request.Since.After(request.Until) || request.Since.UTC().Year() < 0 || request.Since.UTC().Year() > 9999 || request.Until.UTC().Year() < 0 || request.Until.UTC().Year() > 9999 || request.Limit < 1 || request.Limit > MaxRecentLimit || len(request.Service) > 1024 || !utf8.ValidString(request.Service)) {
		return result, fmt.Errorf("invalid page request")
	}
	tx, err := db.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	identity, key, err := cursorMetadata(tx, request.Cursor == "")
	if err != nil {
		return result, err
	}
	var token pageToken
	if request.Cursor != "" {
		token, err = decodePageToken(request.Cursor, key)
		if err != nil {
			return result, err
		}
		if token.Database != identity || (request.Signal != "" && request.Signal != token.Signal) {
			return result, fmt.Errorf("cursor does not match database or signal")
		}
		if request.Service != "" || !request.Since.IsZero() || !request.Until.IsZero() || request.Limit != 0 {
			return result, fmt.Errorf("cursor cannot be combined with filters")
		}
	} else {
		token = pageToken{Version: 1, Database: identity, Signal: request.Signal, Service: request.Service, Since: request.Since.UTC(), Until: request.Until.UTC(), Limit: request.Limit}
	}
	table := token.Signal
	if request.Cursor == "" {
		if err := tx.QueryRow("SELECT coalesce(max(cursor_id),0), count(*) FROM "+table).Scan(&token.HighWater, &token.Count); err != nil {
			return result, err
		}
	} else {
		var count int64
		if err := tx.QueryRow("SELECT count(*) FROM "+table+" WHERE cursor_id <= ?", token.HighWater).Scan(&count); err != nil {
			return result, err
		}
		if count != token.Count {
			return result, fmt.Errorf("cursor invalidated by deletion or rebuild")
		}
	}
	start, end := projectedWindow(token.Since, token.Until)
	opts := QueryOptions{Service: token.Service, Since: start, Until: end, Limit: token.Limit + 1}
	position := &pagePosition{highWater: token.HighWater, lastTime: token.LastTime, lastID: token.LastID}
	result = PageResult{Signal: token.Signal, Since: token.Since, Until: token.Until}
	var lastID int64
	var lastTime time.Time
	var more bool
	if token.Signal == "traces" {
		rows, err := queryTraces(tx, TraceQueryOptions{QueryOptions: opts}, false, position)
		if err != nil {
			return result, err
		}
		more = len(rows) > token.Limit
		if more {
			rows = rows[:token.Limit]
		}
		if rows == nil {
			rows = []TraceResult{}
		}
		result.Records = rows
		if len(rows) > 0 {
			lastID = rows[len(rows)-1].cursorID
			lastTime = rows[len(rows)-1].StartTime
		}
	} else {
		rows, err := queryLogs(tx, LogQueryOptions{QueryOptions: opts}, false, position)
		if err != nil {
			return result, err
		}
		more = len(rows) > token.Limit
		if more {
			rows = rows[:token.Limit]
		}
		if rows == nil {
			rows = []LogResult{}
		}
		result.Records = rows
		if len(rows) > 0 {
			lastID = rows[len(rows)-1].cursorID
			lastTime = rows[len(rows)-1].Timestamp
		}
	}
	if more {
		token.LastID, token.LastTime = lastID, lastTime.UTC()
		result.NextCursor, err = encodePageToken(token, key)
		if err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
