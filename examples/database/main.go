// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lemon4ksan/g-man/pkg/storage"
)

// PostgresProvider implements storage.Provider backed by a PostgreSQL database table.
type PostgresProvider struct {
	db *sql.DB
}

// NewPostgresProvider constructs a PostgresProvider, configures connection pools, and ensures table schema creation.
func NewPostgresProvider(connStr string) (*PostgresProvider, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	query := `
	CREATE TABLE IF NOT EXISTS gman_storage (
		namespace VARCHAR(128) NOT NULL,
		key VARCHAR(256) NOT NULL,
		value BYTEA NOT NULL,
		updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
		PRIMARY KEY (namespace, key)
	);`

	if _, err := db.ExecContext(ctx, query); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &PostgresProvider{db: db}, nil
}

func (p *PostgresProvider) KV(namespace string) storage.KV {
	return &postgresKV{
		db:        p.db,
		namespace: namespace,
	}
}

func (p *PostgresProvider) Close() error {
	return p.db.Close()
}

type postgresKV struct {
	db        *sql.DB
	namespace string
}

func (kv *postgresKV) Set(ctx context.Context, key string, value []byte) error {
	tx, err := kv.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	query := `
	INSERT INTO gman_storage (namespace, key, value, updated_at)
	VALUES ($1, $2, $3, NOW())
	ON CONFLICT (namespace, key)
	DO UPDATE SET value = EXCLUDED.value, updated_at = NOW();`

	if _, err := tx.ExecContext(ctx, query, kv.namespace, key, value); err != nil {
		return err
	}

	return tx.Commit()
}

func (kv *postgresKV) Get(ctx context.Context, key string) ([]byte, error) {
	query := `SELECT value FROM gman_storage WHERE namespace = $1 AND key = $2;`

	var val []byte

	err := kv.db.QueryRowContext(ctx, query, kv.namespace, key).Scan(&val)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}

		return nil, err
	}

	return val, nil
}

func (kv *postgresKV) Delete(ctx context.Context, key string) error {
	query := `DELETE FROM gman_storage WHERE namespace = $1 AND key = $2;`

	_, err := kv.db.ExecContext(ctx, query, kv.namespace, key)

	return err
}

func (kv *postgresKV) Has(ctx context.Context, key string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM gman_storage WHERE namespace = $1 AND key = $2);`

	var exists bool

	err := kv.db.QueryRowContext(ctx, query, kv.namespace, key).Scan(&exists)

	return exists, err
}

func (kv *postgresKV) Keys(ctx context.Context, prefix string) ([]string, error) {
	query := `
	SELECT key FROM gman_storage
	WHERE namespace = $1 AND key LIKE $2
	ORDER BY key ASC;`

	rows, err := kv.db.QueryContext(ctx, query, kv.namespace, prefix+"%")
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var keys []string

	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}

		keys = append(keys, k)
	}

	return keys, nil
}
