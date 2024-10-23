package db

import (
	"context"
	"github.com/jackc/pgx/v4"
	"log"
)

func IsPostgresInstalled() bool {
	conn, err := pgx.Connect(context.Background(), "postgres://postgres:postgres@localhost:5432/postgres")

	if err != nil {
		log.Printf("Failed to connect to postgres: %v", err)
		return false
	}

	defer conn.Close(context.Background())

	return true
}
