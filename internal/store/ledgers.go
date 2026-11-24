package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// A ledger holds entries and their types and cards. Its owner can share it:
// an editor changes it as the owner does, a viewer only reads it. Everyone
// starts with a personal ledger and can make more.

const (
	RoleOwner  = "owner"
	RoleEditor = "editor"
	RoleViewer = "viewer"

	maxLedgerName = 60
)

var (
	ErrNotMember    = errors.New("you can't see this ledger")
	ErrLastLedger   = errors.New("this is your only ledger; make another before deleting it")
	ErrAlreadyIn    = errors.New("they already have this ledger")
	ErrOwnerRole    = errors.New("the owner's role can't be changed or removed")
	currencyCodeRe  = regexp.MustCompile(`^[A-Z]{3}$`)
	errLedgerName   = fmt.Errorf("a ledger needs a name of 1–%d characters", maxLedgerName)
	errLedgerMoney  = errors.New("the currency must be an ISO 4217 code, such as CNY or USD")
	errLedgerRole   = errors.New("the role must be editor or viewer")
	errNoSuchPerson = errors.New("there is no account with this email on this server; an admin can add one")
)

// Ledger is a ledger as one member sees it.
type Ledger struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Currency  string `json:"currency"`
	Role      string `json:"role"`    // the member's: owner, editor or viewer
	Owner     string `json:"owner"`   // the owner's name, or email
	Members   int    `json:"members"` // how many people have it, the owner included
	CreatedAt string `json:"created_at"`
}

// CanWrite says whether the member may change the ledger's entries, types and cards.
func (l *Ledger) CanWrite() bool { return l.Role == RoleOwner || l.Role == RoleEditor }

// LedgerMember is one person who has a ledger.
type LedgerMember struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
}

func cleanLedgerName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxLedgerName {
		return "", errLedgerName
	}
	return name, nil
}

func cleanCurrency(c string) (string, error) {
	c = strings.ToUpper(strings.TrimSpace(c))
	if !currencyCodeRe.MatchString(c) {
		return "", errLedgerMoney
	}
	return c, nil
}

// CreateLedger makes a ledger owned by ownerID, with the usual types and cards.
func (s *Store) CreateLedger(ctx context.Context, ownerID int64, name, currency string) (*Ledger, error) {
	name, err := cleanLedgerName(name)
	if err != nil {
		return nil, err
	}
	if currency, err = cleanCurrency(currency); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ts := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO ledgers (owner_id, name, currency, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, ownerID, name, currency, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_members (ledger_id, user_id, role, created_at) VALUES (?, ?, 'owner', ?)`, id, ownerID, ts); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := s.seedOptions(ctx, id); err != nil {
		return nil, err
	}
	return s.GetLedger(ctx, ownerID, id)
}

const ledgerSelect = `SELECT l.id, l.name, l.currency, m.role, COALESCE(NULLIF(o.name, ''), o.email),
		(SELECT COUNT(*) FROM ledger_members x WHERE x.ledger_id = l.id), l.created_at
	FROM ledgers l JOIN ledger_members m ON m.ledger_id = l.id JOIN users o ON o.id = l.owner_id`

func scanLedger(row interface{ Scan(...any) error }) (*Ledger, error) {
	var l Ledger
	if err := row.Scan(&l.ID, &l.Name, &l.Currency, &l.Role, &l.Owner, &l.Members, &l.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotMember
		}
		return nil, err
	}
	return &l, nil
}

// ListLedgers lists the ledgers userID has: their own first, oldest first.
func (s *Store) ListLedgers(ctx context.Context, userID int64) ([]Ledger, error) {
	rows, err := s.db.QueryContext(ctx, ledgerSelect+` WHERE m.user_id = ? ORDER BY m.role != 'owner', l.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ledger{}
	for rows.Next() {
		l, err := scanLedger(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// GetLedger is a ledger as userID sees it; ErrNotMember if they don't have it.
func (s *Store) GetLedger(ctx context.Context, userID, ledgerID int64) (*Ledger, error) {
	return scanLedger(s.db.QueryRowContext(ctx, ledgerSelect+` WHERE m.user_id = ? AND l.id = ?`, userID, ledgerID))
}

// UpdateLedger renames a ledger or changes its currency (the owner's to do).
func (s *Store) UpdateLedger(ctx context.Context, ledgerID int64, name, currency *string) error {
	if name != nil {
		n, err := cleanLedgerName(*name)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE ledgers SET name = ?, updated_at = ? WHERE id = ?`, n, now(), ledgerID); err != nil {
			return err
		}
	}
	if currency != nil {
		c, err := cleanCurrency(*currency)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE ledgers SET currency = ?, updated_at = ? WHERE id = ?`, c, now(), ledgerID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteLedger deletes a ledger with its entries, types and cards (the
// owner's to do), unless it's the last ledger the owner has.
func (s *Store) DeleteLedger(ctx context.Context, ownerID, ledgerID int64) error {
	var others int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_members WHERE user_id = ? AND ledger_id != ?`, ownerID, ledgerID).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return ErrLastLedger
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM ledgers WHERE id = ? AND owner_id = ?`, ledgerID, ownerID)
	return err
}

// ListLedgerMembers lists who has a ledger: the owner first.
func (s *Store) ListLedgerMembers(ctx context.Context, ledgerID int64) ([]LedgerMember, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.email, u.name, m.role FROM ledger_members m JOIN users u ON u.id = m.user_id
		WHERE m.ledger_id = ? ORDER BY m.role != 'owner', m.created_at, u.email`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LedgerMember{}
	for rows.Next() {
		var m LedgerMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func cleanRole(role string) (string, error) {
	if role != RoleEditor && role != RoleViewer {
		return "", errLedgerRole
	}
	return role, nil
}

// AddLedgerMember gives the ledger to the account with this email, as an editor or a viewer.
func (s *Store) AddLedgerMember(ctx context.Context, ledgerID int64, email, role string) (*LedgerMember, error) {
	role, err := cleanRole(role)
	if err != nil {
		return nil, err
	}
	email, err = NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	var m LedgerMember
	err = s.db.QueryRowContext(ctx, `SELECT id, email, name FROM users WHERE email = ?`, email).Scan(&m.UserID, &m.Email, &m.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoSuchPerson
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO ledger_members (ledger_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`, ledgerID, m.UserID, role, now()); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return nil, ErrAlreadyIn
		}
		return nil, err
	}
	m.Role = role
	return &m, nil
}

// SetLedgerMemberRole makes a member an editor or a viewer; the owner stays the owner.
func (s *Store) SetLedgerMemberRole(ctx context.Context, ledgerID, userID int64, role string) error {
	role, err := cleanRole(role)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE ledger_members SET role = ? WHERE ledger_id = ? AND user_id = ? AND role != 'owner'`, role, ledgerID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrOwnerRole
	}
	return nil
}

// RemoveLedgerMember takes the ledger from a member (or a member leaves it); the owner can't be removed.
func (s *Store) RemoveLedgerMember(ctx context.Context, ledgerID, userID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ledger_members WHERE ledger_id = ? AND user_id = ? AND role != 'owner'`, ledgerID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrOwnerRole
	}
	return nil
}
