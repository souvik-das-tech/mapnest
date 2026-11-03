package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Session is one admin UI sign-in (migration 0004). IDHash is the hex
// SHA-256 of the random session id; the id itself is never stored.
type Session struct {
	IDHash     string
	Username   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	ClientIP   string
	UserAgent  string
}

// CreateSession stores a new session.
func (s *Store) CreateSession(ctx context.Context, ss Session) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions
		(id_hash, username, created_at, expires_at, last_seen_at, client_ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ss.IDHash, ss.Username, ss.CreatedAt.Unix(), ss.ExpiresAt.Unix(), ss.LastSeenAt.Unix(),
		ss.ClientIP, truncate(ss.UserAgent, 256))
	return err
}

// GetSession returns the session, or ok=false if there is none.
func (s *Store) GetSession(ctx context.Context, idHash string) (Session, bool, error) {
	var (
		ss                 Session
		created, exp, seen int64
		ip, ua             sql.NullString
	)
	err := s.DB.QueryRowContext(ctx, `SELECT id_hash, username, created_at, expires_at, last_seen_at, client_ip, user_agent
		FROM sessions WHERE id_hash = ?`, idHash).
		Scan(&ss.IDHash, &ss.Username, &created, &exp, &seen, &ip, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	ss.CreatedAt, ss.ExpiresAt, ss.LastSeenAt = time.Unix(created, 0), time.Unix(exp, 0), time.Unix(seen, 0)
	ss.ClientIP, ss.UserAgent = ip.String, ua.String
	return ss, true, nil
}

// TouchSession records activity for the idle timeout.
func (s *Store) TouchSession(ctx context.Context, idHash string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id_hash = ?`, at.Unix(), idHash)
	return err
}

// DeleteSession signs one session out.
func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, idHash)
	return err
}

// DeleteAllSessions signs everybody out and returns how many were removed.
func (s *Store) DeleteAllSessions(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sessions`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurgeSessions removes sessions past their expiry or idle for longer than idle.
func (s *Store) PurgeSessions(ctx context.Context, now time.Time, idle time.Duration) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ? OR last_seen_at <= ?`,
		now.Unix(), now.Add(-idle).Unix())
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
