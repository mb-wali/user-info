package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/cyverse-de/queries"
	"github.com/gorilla/mux"
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

// SavedSearchesApp is an implementation of the App interface created to manage
// saved-searches
type SavedSearchesApp struct {
	searches seDB
	router   *mux.Router
}

// NewSearchesApp returns a new *SavedSearchesApp
func NewSearchesApp(db seDB, router *mux.Router) *SavedSearchesApp {
	searchesApp := &SavedSearchesApp{
		searches: db,
		router:   router,
	}
	router.HandleFunc("/searches/", searchesApp.Greeting).Methods("GET")
	router.HandleFunc("/searches/{username}", searchesApp.GetRequest).Methods("GET")
	router.HandleFunc("/searches/{username}", searchesApp.PutRequest).Methods("PUT")
	router.HandleFunc("/searches/{username}", searchesApp.PostRequest).Methods("POST")
	router.HandleFunc("/searches/{username}", searchesApp.DeleteRequest).Methods("DELETE")
	router.Handle("/debug/vars", http.DefaultServeMux)
	return searchesApp
}

// Greeting prints out a greeting to the writer from saved-searches.
func (s *SavedSearchesApp) Greeting(writer http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(writer, "Hello from saved-searches.\n")
}

// GetRequest handles writing out a user's saved searches as a response.
func (s *SavedSearchesApp) GetRequest(writer http.ResponseWriter, r *http.Request) {
	var (
		username   string
		userExists bool
		err        error
		ok         bool
		searches   []string
		v          = mux.Vars(r)
	)

	if username, ok = v["username"]; !ok {
		badRequest(writer, "Missing username in URL")
		return
	}

	if userExists, err = s.searches.isUser(username); err != nil {
		badRequest(writer, fmt.Sprintf("Error checking for username %s: %s", username, err))
		return
	}

	if !userExists {
		handleNonUser(writer, username)
		return
	}

	if searches, err = s.searches.getSavedSearches(username); err != nil {
		errored(writer, err.Error())
		return
	}

	if len(searches) < 1 {
		fmt.Fprintf(writer, "{}")
		return
	}

	fmt.Fprintf(writer, searches[0])
}

// PutRequest handles creating new user saved searches.
func (s *SavedSearchesApp) PutRequest(writer http.ResponseWriter, r *http.Request) {
	s.PostRequest(writer, r)
}

// PostRequest handles modifying an existing user's saved searches.
func (s *SavedSearchesApp) PostRequest(writer http.ResponseWriter, r *http.Request) {
	var (
		username    string
		userExists  bool
		hasSearches bool
		err         error
		ok          bool
		v           = mux.Vars(r)
	)

	if username, ok = v["username"]; !ok {
		badRequest(writer, "Missing username in URL")
		return
	}

	bodyBuffer, err := ioutil.ReadAll(r.Body)
	if err != nil {
		errored(writer, fmt.Sprintf("Error reading body: %s", err))
		return
	}

	// Make sure valid JSON was uploaded in the body.
	var parsedBody interface{}
	if err = json.Unmarshal(bodyBuffer, &parsedBody); err != nil {
		badRequest(writer, fmt.Sprintf("Error parsing body: %s", err.Error()))
		return
	}

	bodyString := string(bodyBuffer)

	if userExists, err = s.searches.isUser(username); err != nil {
		badRequest(writer, fmt.Sprintf("Error checking for username %s: %s", username, err))
		return
	}

	if !userExists {
		handleNonUser(writer, username)
		return
	}

	if hasSearches, err = s.searches.hasSavedSearches(username); err != nil {
		errored(writer, err.Error())
		return
	}

	var upsert func(string, string) error
	if hasSearches {
		upsert = s.searches.updateSavedSearches
	} else {
		upsert = s.searches.insertSavedSearches
	}
	if err = upsert(username, bodyString); err != nil {
		errored(writer, err.Error())
		return
	}

	retval := map[string]interface{}{
		"saved_searches": parsedBody,
	}
	jsoned, err := json.Marshal(retval)
	if err != nil {
		errored(writer, err.Error())
		return
	}

	writer.Write(jsoned)
}

// DeleteRequest handles deleting a user's saved searches.
func (s *SavedSearchesApp) DeleteRequest(writer http.ResponseWriter, r *http.Request) {
	var (
		err        error
		ok         bool
		userExists bool
		username   string
		v          = mux.Vars(r)
	)

	if username, ok = v["username"]; !ok {
		badRequest(writer, "Missing username in URL")
		return
	}

	if userExists, err = s.searches.isUser(username); err != nil {
		badRequest(writer, fmt.Sprintf("Error checking for username %s: %s", username, err))
		return
	}

	if !userExists {
		return
	}

	if err = s.searches.deleteSavedSearches(username); err != nil {
		errored(writer, err.Error())
	}
}
