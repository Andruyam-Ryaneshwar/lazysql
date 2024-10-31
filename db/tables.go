package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v4"
	"log"
	"strings"
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

func GetTableData(conn *pgx.Conn, tableName string) ([]map[string]interface{}, error) {
	// Sanitize the table name to prevent SQL injection
	sql := fmt.Sprintf("SELECT * FROM %s", pgx.Identifier{tableName}.Sanitize())

	rows, err := conn.Query(context.Background(), sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Get column names
	fieldDescriptions := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescriptions))
	for i, fd := range fieldDescriptions {
		columns[i] = string(fd.Name)
	}

	// Collect rows
	var data []map[string]interface{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		data = append(data, row)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return data, nil
}

func GetTableColumns(conn *pgx.Conn, tableName string) ([]string, error) {
	sql := fmt.Sprintf("SELECT column_name FROM information_schema.columns WHERE table_name='%s'", tableName)
	rows, err := conn.Query(context.Background(), sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, err
		}
		columns = append(columns, columnName)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return columns, nil
}

func InsertRow(conn *pgx.Conn, tableName string, values map[string]interface{}) error {
	columns := make([]string, 0, len(values))
	placeholders := make([]string, 0, len(values))
	args := make([]interface{}, 0, len(values))

	i := 1
	for col, val := range values {
		columns = append(columns, col)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		args = append(args, val)
		i++
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", pgx.Identifier{tableName}.Sanitize(), strings.Join(columns, ","), strings.Join(placeholders, ","))
	_, err := conn.Exec(context.Background(), sql, args...)
	return err
}
