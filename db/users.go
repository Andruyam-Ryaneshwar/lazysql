package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v4"
	"log"
)

func GetUsers(conn *pgx.Conn) ([]string, error) {
	cursor, err := conn.Query(context.Background(), "SELECT usename FROM pg_catalog.pg_user")
	defer cursor.Close()

	if err != nil {
		log.Printf("Error fetching users: %v", err)
		return nil, err
	}

	var users []string

	for cursor.Next() {
		var user string

		if err := cursor.Scan(&user); err != nil {
			log.Printf("Error scanning user: %v", err)
			return nil, err
		}

		users = append(users, user)
	}

	return users, nil
}

func CreateUser(conn *pgx.Conn, username string, password string) error {
	sqlQuery := fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s'", pgx.Identifier{username}.Sanitize(), password)
	_, err := conn.Exec(context.Background(), sqlQuery)

	if err != nil {
		log.Printf("Error while creating user: %v", err)
		return err
	}

	log.Printf("User created successfully: %v", username)
	return nil
}

func ConnectAsUser(username string, password string, database string) (*pgx.Conn, error) {
	connStr := fmt.Sprintf("postgres://%s:%s@localhost/%s", username, password, database)
	conn, err := pgx.Connect(context.Background(), connStr)

	if err != nil {
		log.Printf("Error while making a connection: %v", err)
		return nil, err
	}

	log.Printf("Successfully connected as %s user to %s database", username, database)
	return conn, nil
}
