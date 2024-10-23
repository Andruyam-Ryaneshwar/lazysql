package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v4"
	"log"
)

func GetTables(conn *pgx.Conn) ([]string, error) {
	cursor, err := conn.Query(context.Background(), "SELECT tablename FROM pg_tables WHERE schemaname = 'public'")
	defer cursor.Close()

	if err != nil {
		log.Printf("Error querying tables: %v", err)
		return nil, err
	}

	var tables []string

	for cursor.Next() {
		var tableName string
		if err := cursor.Scan(&tableName); err != nil {
			log.Printf("Error while scanning table: %v", err)
			return nil, err
		}
		tables = append(tables, tableName)
	}

	return tables, nil
}

func CreateTable(conn *pgx.Conn, tableName string, schema string) error {
	query := fmt.Sprintf("CREATE TABLE %s (%s)", pgx.Identifier{tableName}.Sanitize(), schema)
	_, err := conn.Exec(context.Background(), query)

	if err != nil {
		log.Printf("Error while creating table: %v", err)
		return err
	}

	log.Printf("Successfully created a table: %v", tableName)
	return nil
}
