package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"golang.org/x/crypto/bcrypt"
	"os"
	"strings"
	"time"
)

var ErrDenied = errors.New("access denied")
var ErrInvalid = errors.New("invalid account input")

type Service struct {
	Pool          *pgxpool.Pool
	secureCookies bool
}
type Session struct {
	Token string
	CSRF  string
}
type Credentials struct {
	Username string
	Password string
}

func NewService(p *pgxpool.Pool) *Service {
	return &Service{Pool: p, secureCookies: os.Getenv("RAPTOR_SECURE_COOKIES") == "true"}
}
func digest(s string) string   { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func csrf(token string) string { return digest(token + ":csrf") }
func randomToken() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}

func (s *Service) CreateUser(ctx context.Context, a domain.User, u, p, r string) (domain.User, error) {
	if e := RequireAdmin(a); e != nil {
		return domain.User{}, e
	}
	if strings.TrimSpace(u) != u || u == "" || len(u) > 80 || len(p) < 12 || len(p) > 72 || (r != "admin" && r != "user") {
		return domain.User{}, ErrInvalid
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(p), 12)
	if e != nil {
		return domain.User{}, ErrInvalid
	}
	user := domain.User{ID: domain.NewID(), Username: u, Role: r}
	_, e = s.Pool.Exec(ctx, "INSERT INTO raptor.users(id,username,password_hash,role) VALUES($1,$2,$3,$4)", user.ID, u, string(hash), r)
	if e != nil {
		return domain.User{}, errors.New("account could not be saved")
	}
	return user, nil
}
func (s *Service) Login(ctx context.Context, u, p string) (Session, error) {
	var id, hash string
	if e := s.Pool.QueryRow(ctx, "SELECT id,password_hash FROM raptor.users WHERE username=$1", u).Scan(&id, &hash); e != nil {
		return Session{}, ErrDenied
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(p)) != nil {
		return Session{}, ErrDenied
	}
	session := Session{Token: randomToken()}
	session.CSRF = csrf(session.Token)
	_, e := s.Pool.Exec(ctx, "INSERT INTO raptor.sessions(token_hash,user_id,csrf_hash,expires_at) VALUES($1,$2,$3,$4)", digest(session.Token), id, digest(session.CSRF), time.Now().Add(8*time.Hour))
	if e != nil {
		return Session{}, errors.New("session unavailable")
	}
	return session, nil
}
func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if len(token) != 64 {
		return domain.User{}, ErrDenied
	}
	var u domain.User
	e := s.Pool.QueryRow(ctx, "SELECT u.id,u.username,u.role FROM raptor.sessions s JOIN raptor.users u ON u.id=s.user_id WHERE token_hash=$1 AND expires_at>now()", digest(token)).Scan(&u.ID, &u.Username, &u.Role)
	if e != nil {
		return domain.User{}, ErrDenied
	}
	return u, nil
}
func (s *Service) Logout(ctx context.Context, token string) error {
	_, e := s.Pool.Exec(ctx, "DELETE FROM raptor.sessions WHERE token_hash=$1", digest(token))
	return e
}
func RequireAdmin(u domain.User) error {
	if u.Role != "admin" {
		return ErrDenied
	}
	return nil
}
func (s *Service) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, e := s.Pool.Query(ctx, "SELECT id,username,role FROM raptor.users ORDER BY username")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.User{}
	for rows.Next() {
		var u domain.User
		if e = rows.Scan(&u.ID, &u.Username, &u.Role); e != nil {
			return nil, e
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
