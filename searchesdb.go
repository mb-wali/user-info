package main

import (
	"database/sql"

	"github.com/cyverse-de/queries"
)

// seDB defines the interface for interacting with storage. Mostly included
// to make unit tests easier to write.
type seDB interface {
	isUser(string) (bool, error)
	hasSavedSearches(string) (bool, error)
	getSavedSearches(string) ([]string, error)
	insertSavedSearches(string, string) error
	updateSavedSearches(string, string) error
	deleteSavedSearches(string) error
}

// SearchesDB implements the DB interface for interacting with the saved-searches
// database.
type SearchesDB struct {
	db *sql.DB
}

// NewSearchesDB returns a new *SearchesDB.
func NewSearchesDB(db *sql.DB) *SearchesDB {
	return &SearchesDB{
		db: db,
	}
}

// isUser returns whether or not the user exists in the saved searches database.
func (se *SearchesDB) isUser(username string) (bool, error) {
	return queries.IsUser(se.db, username)
}

// hasSavedSearches returns whether or not the given user has saved searches already.
func (se *SearchesDB) hasSavedSearches(username string) (bool, error) {
	var (
		err    error
		exists bool
	)

	query := `SELECT EXISTS(
              SELECT 1
                FROM user_saved_searches s,
                     users u
               WHERE s.user_id = u.id
                 AND u.username = $1) AS exists`

	if err = se.db.QueryRow(query, username).Scan(&exists); err != nil {
		return false, err
	}

	return exists, nil
}

// getSavedSearches returns all of the saved searches associated with the
// provided username.
func (se *SearchesDB) getSavedSearches(username string) ([]string, error) {
	var (
		err    error
		retval []string
		rows   *sql.Rows
	)

	query := `SELECT s.saved_searches saved_searches
              FROM user_saved_searches s,
                   users u
             WHERE s.user_id = u.id
               AND u.username = $1`

	if rows, err = se.db.Query(query, username); err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var search string
		if err = rows.Scan(&search); err != nil {
			return nil, err
		}
		retval = append(retval, search)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return retval, nil
}

// insertSavedSearches adds new saved searches to the database for the user.
func (se *SearchesDB) insertSavedSearches(username, searches string) error {
	var (
		err    error
		userID string
	)

	query := `INSERT INTO user_saved_searches (user_id, saved_searches) VALUES ($1, $2)`

	if userID, err = queries.UserID(se.db, username); err != nil {
		return err
	}

	_, err = se.db.Exec(query, userID, searches)
	return err
}

// updateSavedSearches updates the saved searches in the database for the user.
func (se *SearchesDB) updateSavedSearches(username, searches string) error {
	var (
		err    error
		userID string
	)

	query := `UPDATE ONLY user_saved_searches SET saved_searches = $2 WHERE user_id = $1`

	if userID, err = queries.UserID(se.db, username); err != nil {
		return err
	}

	_, err = se.db.Exec(query, userID, searches)
	return err
}

// deleteSavedSearches removes the user's saved sessions from the database.
func (se *SearchesDB) deleteSavedSearches(username string) error {
	var (
		err    error
		userID string
	)

	query := `DELETE FROM ONLY user_saved_searches WHERE user_id = $1`

	if userID, err = queries.UserID(se.db, username); err != nil {
		return nil
	}

	_, err = se.db.Exec(query, userID)
	return err
}
