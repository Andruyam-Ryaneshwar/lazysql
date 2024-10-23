package db

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v4"
)

func GetDatabases(conn *pgx.Conn) ([]string, error) {
	cursor, err := conn.Query(context.Background(), "SELECT datname FROM pg_database WHERE datistemplate = false")
	defer cursor.Close()

	if err != nil {
		log.Printf("Error querying databses: %v", err)
		return nil, err
	}

	var databases []string

	for cursor.Next() {
		var dbName string
		if err := cursor.Scan(&dbName); err != nil {
			log.Printf("Error while scanning database name: %v", err)
			return nil, err
		}
		databases = append(databases, dbName)
	}

	return databases, nil
}

func CreateDatabase(conn *pgx.Conn, dbName string) error {
	query := fmt.Sprintf("CREATE DATABASE %s", pgx.Identifier{dbName}.Sanitize())
	_, err := conn.Exec(context.Background(), query)

	if err != nil {
		log.Printf("Error creating database: %v", err)
		return err
	}

	log.Printf("Successfully created database: %v", dbName)
	return nil
}
