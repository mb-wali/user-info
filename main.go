package main

import (
	"database/sql"
	"encoding/json"
	_ "expvar"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/cyverse-de/configurate"
	"github.com/cyverse-de/dbutil"
	"github.com/cyverse-de/queries"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// App defines the interface for a user-info application
type App interface {
	Greeting(http.ResponseWriter, *http.Request)
	GetRequest(http.ResponseWriter, *http.Request)
	PutRequest(http.ResponseWriter, *http.Request)
	PostRequest(http.ResponseWriter, *http.Request)
	DeleteRequest(http.ResponseWriter, *http.Request)
}

// -------- START USER PREFERENCES --------
// UserPreferencesRecord represents a user's preferences stored in the database
type UserPreferencesRecord struct {
	ID          string
	Preferences string
	UserID      string
}

// convert makes sure that the JSON has the correct format. "wrap" tells convert
// whether to wrap the object in a map with "preferences" as the key.
func convertPrefs(record *UserPreferencesRecord, wrap bool) (map[string]interface{}, error) {
	var values map[string]interface{}

	if record.Preferences != "" {
		if err := json.Unmarshal([]byte(record.Preferences), &values); err != nil {
			return nil, err
		}
	}

	// We don't want the return value wrapped in a preferences object, so unwrap it
	// if it is wrapped.
	if !wrap {
		if _, ok := values["preferences"]; ok {
			return values["preferences"].(map[string]interface{}), nil
		}
		return values, nil
	}

	// We do want the return value wrapped in a preferences object, so wrap it if it
	// isn't already.
	if _, ok := values["preferences"]; !ok {
		newmap := make(map[string]interface{})
		newmap["preferences"] = values
		return newmap, nil
	}

	return values, nil
}

type pDB interface {
	isUser(username string) (bool, error)

	// DB defines the interface for interacting with the user-prefs database.
	hasPreferences(username string) (bool, error)
	getPreferences(username string) ([]UserPreferencesRecord, error)
	insertPreferences(username, prefs string) error
	updatePreferences(username, prefs string) error
	deletePreferences(username string) error
}

// PrefsDB implements the DB interface for interacting with the user-preferences
// database.
type PrefsDB struct {
	db *sql.DB
}

// NewPrefsDB returns a newly created *PrefsDB.
func NewPrefsDB(db *sql.DB) *PrefsDB {
	return &PrefsDB{
		db: db,
	}
}

// isUser returns whether or not the user exists in the database preferences.
func (p *PrefsDB) isUser(username string) (bool, error) {
	return queries.IsUser(p.db, username)
}

// hasPreferences returns whether or not the given user has preferences already.
func (p *PrefsDB) hasPreferences(username string) (bool, error) {
	query := `SELECT COUNT(p.*)
              FROM user_preferences p,
                   users u
             WHERE p.user_id = u.id
               AND u.username = $1`
	var count int64
	if err := p.db.QueryRow(query, username).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// getPreferences returns a []UserPreferencesRecord of all of the preferences associated
// with the provided username.
func (p *PrefsDB) getPreferences(username string) ([]UserPreferencesRecord, error) {
	query := `SELECT p.id AS id,
                   p.user_id AS user_id,
                   p.preferences AS preferences
              FROM user_preferences p,
                   users u
             WHERE p.user_id = u.id
               AND u.username = $1`

	rows, err := p.db.Query(query, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prefs []UserPreferencesRecord
	for rows.Next() {
		var pref UserPreferencesRecord
		if err := rows.Scan(&pref.ID, &pref.UserID, &pref.Preferences); err != nil {
			return nil, err
		}
		prefs = append(prefs, pref)
	}

	if err := rows.Err(); err != nil {
		return prefs, err
	}

	return prefs, nil
}

// insertPreferences adds a new preferences to the database for the user.
func (p *PrefsDB) insertPreferences(username, prefs string) error {
	query := `INSERT INTO user_preferences (user_id, preferences)
                 VALUES ($1, $2)`
	userID, err := queries.UserID(p.db, username)
	if err != nil {
		return err
	}
	_, err = p.db.Exec(query, userID, prefs)
	return err
}

// updatePreferences updates the preferences in the database for the user.
func (p *PrefsDB) updatePreferences(username, prefs string) error {
	query := `UPDATE ONLY user_preferences
                    SET preferences = $2
                  WHERE user_id = $1`
	userID, err := queries.UserID(p.db, username)
	if err != nil {
		return err
	}
	_, err = p.db.Exec(query, userID, prefs)
	return err
}

// deletePreferences deletes the user's preferences from the database.
func (p *PrefsDB) deletePreferences(username string) error {
	query := `DELETE FROM ONLY user_preferences WHERE user_id = $1`
	userID, err := queries.UserID(p.db, username)
	if err != nil {
		return err
	}
	_, err = p.db.Exec(query, userID)
	return err
}

// UserPreferencesApp is an implementation of the App interface created to manage
// user preferences.
type UserPreferencesApp struct {
	prefs  pDB
	router *mux.Router
}

// New returns a new *UserPreferencesApp
func NewPrefsApp(db pDB, router *mux.Router) *UserPreferencesApp {
	prefsApp := &UserPreferencesApp{
		prefs:  db,
		router: router,
	}
	prefsApp.router.HandleFunc("/", prefsApp.Greeting).Methods("GET")
	prefsApp.router.HandleFunc("/preferences/", prefsApp.Greeting).Methods("GET")
	prefsApp.router.HandleFunc("/preferences/{username}", prefsApp.GetRequest).Methods("GET")
	prefsApp.router.HandleFunc("/preferences/{username}", prefsApp.PutRequest).Methods("PUT")
	prefsApp.router.HandleFunc("/preferences/{username}", prefsApp.PostRequest).Methods("POST")
	prefsApp.router.HandleFunc("/preferences/{username}", prefsApp.DeleteRequest).Methods("DELETE")
	return prefsApp
}

// Greeting prints out a greeting to the writer from user-prefs.
func (u *UserPreferencesApp) Greeting(writer http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(writer, "Hello from user-info.\n")
}

func (u *UserPreferencesApp) getUserPreferencesForRequest(username string, wrap bool) ([]byte, error) {
	var retval UserPreferencesRecord

	prefs, err := u.prefs.getPreferences(username)
	if err != nil {
		return nil, fmt.Errorf("Error getting preferences for username %s: %s", username, err)
	}

	if len(prefs) >= 1 {
		retval = prefs[0]
	}

	response, err := convertPrefs(&retval, wrap)
	if err != nil {
		return nil, fmt.Errorf("Error generating response for username %s: %s", username, err)
	}

	var jsoned []byte
	if len(response) > 0 {
		jsoned, err = json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("Error generating preferences JSON for user %s: %s", username, err)
		}
	} else {
		jsoned = []byte("{}")
	}

	return jsoned, nil
}

// GetRequest handles writing out a user's preferences as a response.
func (u *UserPreferencesApp) GetRequest(writer http.ResponseWriter, r *http.Request) {
	var (
		username   string
		userExists bool
		err        error
		ok         bool
		v          = mux.Vars(r)
	)

	if username, ok = v["username"]; !ok {
		badRequest(writer, "Missing username in URL")
		return
	}

	log.WithFields(log.Fields{
		"service": "preferences",
	}).Info("Getting user preferences for %s", username)
	if userExists, err = u.prefs.isUser(username); err != nil {
		badRequest(writer, fmt.Sprintf("Error checking for username %s: %s", username, err))
		return
	}

	if !userExists {
		handleNonUser(writer, username)
		return
	}

	jsoned, err := u.getUserPreferencesForRequest(username, false)
	if err != nil {
		errored(writer, err.Error())
	}

	writer.Write(jsoned)
}

// PutRequest handles creating new user preferences.
func (u *UserPreferencesApp) PutRequest(writer http.ResponseWriter, r *http.Request) {
	u.PostRequest(writer, r)
}

// PostRequest handles modifying an existing user's preferences.
func (u *UserPreferencesApp) PostRequest(writer http.ResponseWriter, r *http.Request) {
	var (
		username   string
		userExists bool
		hasPrefs   bool
		err        error
		ok         bool
		v          = mux.Vars(r)
	)

	if username, ok = v["username"]; !ok {
		badRequest(writer, "Missing username in URL")
		return
	}

	if userExists, err = u.prefs.isUser(username); err != nil {
		badRequest(writer, fmt.Sprintf("Error checking for username %s: %s", username, err))
		return
	}

	if !userExists {
		handleNonUser(writer, username)
		return
	}

	if hasPrefs, err = u.prefs.hasPreferences(username); err != nil {
		errored(writer, fmt.Sprintf("Error checking preferences for user %s: %s", username, err))
		return
	}

	var checked map[string]interface{}
	bodyBuffer, err := ioutil.ReadAll(r.Body)
	if err != nil {
		errored(writer, fmt.Sprintf("Error reading body: %s", err))
		return
	}

	if err = json.Unmarshal(bodyBuffer, &checked); err != nil {
		errored(writer, fmt.Sprintf("Error parsing request body: %s", err))
		return
	}

	bodyString := string(bodyBuffer)
	if !hasPrefs {
		if err = u.prefs.insertPreferences(username, bodyString); err != nil {
			errored(writer, fmt.Sprintf("Error inserting preferences for user %s: %s", username, err))
			return
		}
	} else {
		if err = u.prefs.updatePreferences(username, bodyString); err != nil {
			errored(writer, fmt.Sprintf("Error updating preferences for user %s: %s", username, err))
			return
		}
	}

	jsoned, err := u.getUserPreferencesForRequest(username, true)
	if err != nil {
		errored(writer, err.Error())
		return
	}

	writer.Write(jsoned)
}

// DeleteRequest handles deleting a user's preferences.
func (u *UserPreferencesApp) DeleteRequest(writer http.ResponseWriter, r *http.Request) {
	var (
		username   string
		userExists bool
		hasPrefs   bool
		err        error
		ok         bool
		v          = mux.Vars(r)
	)

	if username, ok = v["username"]; !ok {
		badRequest(writer, "Missing username in URL")
		return
	}

	if userExists, err = u.prefs.isUser(username); err != nil {
		badRequest(writer, fmt.Sprintf("Error checking for username %s: %s", username, err))
		return
	}

	if !userExists {
		handleNonUser(writer, username)
		return
	}

	if hasPrefs, err = u.prefs.hasPreferences(username); err != nil {
		errored(writer, fmt.Sprintf("Error checking preferences for user %s: %s", username, err))
		return
	}

	if !hasPrefs {
		return
	}

	if err = u.prefs.deletePreferences(username); err != nil {
		errored(writer, fmt.Sprintf("Error deleting preferences for user %s: %s", username, err))
	}
}

// -------- END PREFERENCES DATA --------

// -------- SESSIONS DATA --------

// UserSessionRecord represents a user session stored in the database
type UserSessionRecord struct {
	ID      string
	Session string
	UserID  string
}

// convert makes sure that the JSON has the correct format. "wrap" tells convert
// whether to wrap the object in a map with "session" as the key.
func convertSessions(record *UserSessionRecord, wrap bool) (map[string]interface{}, error) {
	var values map[string]interface{}

	if record.Session != "" {
		if err := json.Unmarshal([]byte(record.Session), &values); err != nil {
			return nil, err
		}
	}

	// We don't want the return value wrapped in a session object, so unwrap it
	// if it is wrapped.
	if !wrap {
		if _, ok := values["session"]; ok {
			return values["session"].(map[string]interface{}), nil
		}
		return values, nil
	}

	// We do want the return value wrapped in a session object, so wrap it if it
	// isn't already.
	if _, ok := values["session"]; !ok {
		newmap := make(map[string]interface{})
		newmap["session"] = values
		return newmap, nil
	}

	return values, nil
}

type sDB interface {
	isUser(username string) (bool, error)

	// DB defines the interface for interacting with the user-sessions database.
	hasSessions(username string) (bool, error)
	getSessions(username string) ([]UserSessionRecord, error)
	insertSession(username, session string) error
	updateSession(username, session string) error
	deleteSession(username string) error
}

// SessionsDB handles interacting with the sessions database.
type SessionsDB struct {
	db *sql.DB
}

// NewSessionsDB returns a newly created *SessionsDB
func NewSessionsDB(db *sql.DB) *SessionsDB {
	return &SessionsDB{
		db: db,
	}
}

// isUser returnes whether or not the user is present in the sessions database.
func (s *SessionsDB) isUser(username string) (bool, error) {
	return queries.IsUser(s.db, username)
}

// hasSessions returns whether or not the given user has a session already.
func (s *SessionsDB) hasSessions(username string) (bool, error) {
	query := `SELECT COUNT(s.*)
              FROM user_sessions s,
                   users u
             WHERE s.user_id = u.id
               AND u.username = $1`
	var count int64
	if err := s.db.QueryRow(query, username).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// getSessions returns a []UserSessionRecord of all of the sessions associated
// with the provided username.
func (s *SessionsDB) getSessions(username string) ([]UserSessionRecord, error) {
	query := `SELECT s.id AS id,
                   s.user_id AS user_id,
                   s.session AS session
              FROM user_sessions s,
                   users u
             WHERE s.user_id = u.id
               AND u.username = $1`

	rows, err := s.db.Query(query, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []UserSessionRecord
	for rows.Next() {
		var session UserSessionRecord
		if err := rows.Scan(&session.ID, &session.UserID, &session.Session); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return sessions, err
	}

	return sessions, nil
}

// insertSession adds a new session to the database for the user.
func (s *SessionsDB) insertSession(username, session string) error {
	query := `INSERT INTO user_sessions (user_id, session)
                 VALUES ($1, $2)`
	userID, err := queries.UserID(s.db, username)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(query, userID, session)
	return err
}

// updateSession updates the session in the database for the user.
func (s *SessionsDB) updateSession(username, session string) error {
	query := `UPDATE ONLY user_sessions
                    SET session = $2
                  WHERE user_id = $1`
	userID, err := queries.UserID(s.db, username)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(query, userID, session)
	return err
}

// deleteSession deletes the user's session from the database.
func (s *SessionsDB) deleteSession(username string) error {
	query := `DELETE FROM ONLY user_sessions WHERE user_id = $1`
	userID, err := queries.UserID(s.db, username)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(query, userID)
	return err
}

// UserSessionsApp is an implementation of the App interface created to manage
// user sessions.
type UserSessionsApp struct {
	sessions sDB
	router   *mux.Router
}

// New returns a new *UserSessionsApp
func NewSessionsApp(db sDB, router *mux.Router) *UserSessionsApp {
	sessionsApp := &UserSessionsApp{
		sessions: db,
		router:   router,
	}
	sessionsApp.router.HandleFunc("/sessions/", sessionsApp.Greeting).Methods("GET")
	sessionsApp.router.HandleFunc("/sessions/{username}", sessionsApp.GetRequest).Methods("GET")
	sessionsApp.router.HandleFunc("/sessions/{username}", sessionsApp.PutRequest).Methods("PUT")
	sessionsApp.router.HandleFunc("/sessions/{username}", sessionsApp.PostRequest).Methods("POST")
	sessionsApp.router.HandleFunc("/sessions/{username}", sessionsApp.DeleteRequest).Methods("DELETE")
	return sessionsApp
}

// Greeting prints out a greeting to the writer from user-sessions.
func (u *UserSessionsApp) Greeting(writer http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(writer, "Hello from user-sessions.")
}

func (u *UserSessionsApp) getUserSessionForRequest(username string, wrap bool) ([]byte, error) {
	sessions, err := u.sessions.getSessions(username)
	if err != nil {
		return nil, fmt.Errorf("Error getting sessions for username %s: %s", username, err)
	}

	var retval UserSessionRecord
	if len(sessions) >= 1 {
		retval = sessions[0]
	}

	response, err := convertSessions(&retval, wrap)
	if err != nil {
		return nil, fmt.Errorf("Error generating response for username %s: %s", username, err)
	}

	var jsoned []byte
	if len(response) > 0 {
		jsoned, err = json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("Error generating session JSON for user %s: %s", username, err)
		}
	} else {
		jsoned = []byte("{}")
	}

	return jsoned, nil
}

// GetRequest handles writing out a user's session as a response.
func (u *UserSessionsApp) GetRequest(writer http.ResponseWriter, r *http.Request) {
	var (
		username   string
		userExists bool
		err        error
		ok         bool
		v          = mux.Vars(r)
	)

	if username, ok = v["username"]; !ok {
		badRequest(writer, "Missing username in URL")
		return
	}

	log.WithFields(log.Fields{
		"service": "sessions",
	}).Info("Getting user session for %s", username)
	if userExists, err = u.sessions.isUser(username); err != nil {
		badRequest(writer, fmt.Sprintf("Error checking for username %s: %s", username, err))
		return
	}

	if !userExists {
		badRequest(writer, fmt.Sprintf("User %s does not exist", username))
		return
	}

	jsoned, err := u.getUserSessionForRequest(username, false)
	if err != nil {
		errored(writer, err.Error())
	}

	writer.Write(jsoned)
}

// PutRequest handles creating a new user session.
func (u *UserSessionsApp) PutRequest(writer http.ResponseWriter, r *http.Request) {
	u.PostRequest(writer, r)
}

// PostRequest handles modifying an existing user session.
func (u *UserSessionsApp) PostRequest(writer http.ResponseWriter, r *http.Request) {
	var (
		username   string
		userExists bool
		hasSession bool
		err        error
		ok         bool
		v          = mux.Vars(r)
	)

	if username, ok = v["username"]; !ok {
		badRequest(writer, "Missing username in URL")
		return
	}

	if userExists, err = u.sessions.isUser(username); err != nil {
		badRequest(writer, fmt.Sprintf("Error checking for username %s: %s", username, err))
		return
	}

	if !userExists {
		badRequest(writer, fmt.Sprintf("User %s does not exist", username))
		return
	}

	if hasSession, err = u.sessions.hasSessions(username); err != nil {
		errored(writer, fmt.Sprintf("Error checking session for user %s: %s", username, err))
		return
	}

	var checked map[string]interface{}
	bodyBuffer, err := ioutil.ReadAll(r.Body)
	if err != nil {
		errored(writer, fmt.Sprintf("Error reading body: %s", err))
		return
	}

	if err = json.Unmarshal(bodyBuffer, &checked); err != nil {
		errored(writer, fmt.Sprintf("Error parsing request body: %s", err))
		return
	}

	bodyString := string(bodyBuffer)
	if !hasSession {
		if err = u.sessions.insertSession(username, bodyString); err != nil {
			errored(writer, fmt.Sprintf("Error inserting session for user %s: %s", username, err))
			return
		}
	} else {
		if err = u.sessions.updateSession(username, bodyString); err != nil {
			errored(writer, fmt.Sprintf("Error updating session for user %s: %s", username, err))
			return
		}
	}

	jsoned, err := u.getUserSessionForRequest(username, true)
	if err != nil {
		errored(writer, err.Error())
		return
	}

	writer.Write(jsoned)
}

// DeleteRequest handles deleting a user session.
func (u *UserSessionsApp) DeleteRequest(writer http.ResponseWriter, r *http.Request) {
	var (
		username   string
		userExists bool
		hasSession bool
		err        error
		ok         bool
		v          = mux.Vars(r)
	)

	if username, ok = v["username"]; !ok {
		badRequest(writer, "Missing username in URL")
		return
	}

	if userExists, err = u.sessions.isUser(username); err != nil {
		badRequest(writer, fmt.Sprintf("Error checking for username %s: %s", username, err))
		return
	}

	if !userExists {
		badRequest(writer, fmt.Sprintf("User %s does not exist", username))
		return
	}

	if hasSession, err = u.sessions.hasSessions(username); err != nil {
		errored(writer, fmt.Sprintf("Error checking session for user %s: %s", username, err))
		return
	}

	if !hasSession {
		return
	}

	if err = u.sessions.deleteSession(username); err != nil {
		errored(writer, fmt.Sprintf("Error deleting session for user %s: %s", username, err))
	}
}

// -------- END SESSIONS DATA --------

func badRequest(writer http.ResponseWriter, msg string) {
	http.Error(writer, msg, http.StatusBadRequest)
	log.Error(msg)
}

func errored(writer http.ResponseWriter, msg string) {
	http.Error(writer, msg, http.StatusInternalServerError)
	log.Error(msg)
}

func handleNonUser(writer http.ResponseWriter, username string) {
	var (
		retval []byte
		err    error
	)

	retval, err = json.Marshal(map[string]string{
		"user": username,
	})
	if err != nil {
		errored(writer, fmt.Sprintf("Error generating json for non-user %s", err))
		return
	}

	badRequest(writer, string(retval))

	return
}

func fixAddr(addr string) string {
	if !strings.HasPrefix(addr, ":") {
		return fmt.Sprintf(":%s", addr)
	}
	return addr
}

var (
	gitref  string
	appver  string
	builtby string
)

// AppVersion prints the version information to stdout
func AppVersion() {
	if appver != "" {
		fmt.Printf("App-Version: %s\n", appver)
	}
	if gitref != "" {
		fmt.Printf("Git-Ref: %s\n", gitref)
	}
	if builtby != "" {
		fmt.Printf("Built-By: %s\n", builtby)
	}
}

func main() {
	var (
		showVersion = flag.Bool("version", false, "Print the version information")
		cfgPath     = flag.String("config", "/etc/iplant/de/jobservices.yml", "The path to the config file")
		port        = flag.String("port", "60000", "The port number to listen on")
		err         error
		cfg         *viper.Viper
	)

	flag.Parse()

	if *showVersion {
		AppVersion()
		os.Exit(0)
	}

	if *cfgPath == "" {
		log.Fatal("--config must be set")
	}

	if cfg, err = configurate.InitDefaults(*cfgPath, configurate.JobServicesDefaults); err != nil {
		log.Fatal(err.Error())
	}

	dburi := cfg.GetString("db.uri")
	connector, err := dbutil.NewDefaultConnector("1m")
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Info("Connecting to the database...")
	db, err := connector.Connect("postgres", dburi)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer db.Close()
	log.Info("Connected to the database.")

	if err := db.Ping(); err != nil {
		log.Fatal(err.Error())
	}
	log.Info("Successfully pinged the database")

	router := mux.NewRouter()
	router.Handle("/preferences/debug/vars", http.DefaultServeMux)

	log.Info("Listening on port %s", *port)
	prefsDB := NewPrefsDB(db)
	prefsApp := NewPrefsApp(prefsDB, router)

	log.Info("Listening on port %s", *port)
	sessionsDB := NewSessionsDB(db)
	sessionsApp := NewSessionsApp(sessionsDB, router)

	log.Debug(prefsApp)
	log.Debug(sessionsApp)
	log.Fatal(http.ListenAndServe(fixAddr(*port), router))
}
