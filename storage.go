package main

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage interface {
	CreateAccount(context.Context, *Account) (*Account, error)
	DeleteAccount(context.Context, int) error
	UpdateAccount(context.Context, *Account) error
	GetAccounts(context.Context) ([]*Account, error)
	GetAccountById(context.Context, int) (*Account, error)

	DiscordUserExists(context.Context, string) (bool, error)
	CreateDiscordUser(context.Context, *DiscordUser) error
}

type PostgresStore struct {
	db *pgxpool.Pool
}

func NewPostgresStore(conStr string) (*PostgresStore, error) {
	ctx := context.Background()
	dbpool, err := pgxpool.New(ctx, conStr)
	if err != nil {
		return nil, err
	}

	if err := dbpool.Ping(ctx); err != nil {
		return nil, err
	}

	return &PostgresStore{
		db: dbpool,
	}, nil
}

func (s *PostgresStore) Init() error {
	return s.CreateAccountTable()
}

func (s *PostgresStore) CreateAccountTable() error {
	ctx := context.Background()
	query := `
		create table if not exists account
		( id serial primary key
		, first_name text
		, last_name text
		, number serial
		, balance int
		, created_at timestamptz default (now() at time zone 'utc')
		)`

	_, err := s.db.Exec(ctx, query)
	return err
}

func (s *PostgresStore) CreateAccount(context context.Context, account *Account) (*Account, error) {
	rows, _ := s.db.Query(context,
		`insert into account(first_name, last_name, balance, number, created_at)
		values ($1, $2, $3, $4, $5)
		returning id, first_name, last_name, balance, number, created_at`,
		account.FirstName, account.LastName, account.Balance, account.Number, account.CreatedAt)

	dbAccount, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByNameLax[Account])
	if err != nil {
		return nil, err
	}

	return dbAccount, err
}

func (s *PostgresStore) DeleteAccount(context context.Context, id int) error {
	_, err := s.db.Exec(context, "delete from account where id = $1", id)
	return err
}
func (s *PostgresStore) UpdateAccount(context context.Context, account *Account) error {
	return nil
}

func (s *PostgresStore) GetAccounts(context context.Context) ([]*Account, error) {
	rows, _ := s.db.Query(context, "select * from account")
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByNameLax[Account])
}

func (s *PostgresStore) GetAccountById(context context.Context, id int) (*Account, error) {
	rows, _ := s.db.Query(context, "select * from account where id = $1", id)
	account, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByNameLax[Account])
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // no rows
		}
		return nil, err // unknown err
	}
	return account, nil // no err
}

func (s *PostgresStore) DiscordUserExists(ctx context.Context, id string) (bool, error) {
	err := s.db.QueryRow(ctx, "select 1 from discord_user where id = $1", id).Scan()
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *PostgresStore) CreateDiscordUser(ctx context.Context, user *DiscordUser) error {
	query := "insert into discord_user(id, global_name, avatar) values ($1, $2, $3)"
	_, err := s.db.Exec(ctx, query, user.Id, user.GlobalName, user.Avatar)
	return err
}
