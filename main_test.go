package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
)

type MockDB struct {
	storage map[string]map[string]interface{}
	users   map[string]bool
}

func NewMockDB() *MockDB {
	return &MockDB{
		storage: make(map[string]map[string]interface{}),
		users:   make(map[string]bool),
	}
}

func (m *MockDB) isUser(username string) (bool, error) {
	_, ok := m.users[username]
	return ok, nil
}

func (m *MockDB) hasPreferences(username string) (bool, error) {
	stored, ok := m.storage[username]
	if !ok {
		return false, nil
	}
	if stored == nil {
		return false, nil
	}
	prefs, ok := m.storage[username]["user-prefs"].(string)
	if !ok {
		return false, nil
	}
	if prefs == "" {
		return false, nil
	}
	return true, nil
}

func (m *MockDB) getPreferences(username string) ([]UserPreferencesRecord, error) {
	return []UserPreferencesRecord{
		UserPreferencesRecord{
			ID:          "id",
			Preferences: m.storage[username]["user-prefs"].(string),
			UserID:      "user-id",
		},
	}, nil
}

func (m *MockDB) insertPreferences(username, prefs string) error {
	if _, ok := m.storage[username]["user-prefs"]; !ok {
		m.storage[username] = make(map[string]interface{})
	}
	m.storage[username]["user-prefs"] = prefs
	return nil
}

func (m *MockDB) updatePreferences(username, prefs string) error {
	return m.insertPreferences(username, prefs)
}

func (m *MockDB) deletePreferences(username string) error {
	delete(m.storage, username)
	return nil
}

func TestConvertBlankPreferences(t *testing.T) {
	record := &UserPreferencesRecord{
		ID:          "test_id",
		Preferences: "",
		UserID:      "test_user_id",
	}
	actual, err := convertPrefs(record, false)
	if err != nil {
		t.Error(err)
	}
	if len(actual) > 0 {
		t.Fail()
	}
}

func TestConvertUnparseablePreferences(t *testing.T) {
	record := &UserPreferencesRecord{
		ID:          "test_id",
		Preferences: "------------",
		UserID:      "test_user_id",
	}
	actual, err := convertPrefs(record, false)
	if err == nil {
		t.Fail()
	}
	if actual != nil {
		t.Fail()
	}
}

func TestConvertEmbeddedPreferences(t *testing.T) {
	record := &UserPreferencesRecord{
		ID:          "test_id",
		Preferences: `{"preferences":{"foo":"bar"}}`,
		UserID:      "test_user_id",
	}
	actual, err := convertPrefs(record, false)
	if err != nil {
		t.Fail()
	}
	if _, ok := actual["foo"]; !ok {
		t.Fail()
	}
	if actual["foo"].(string) != "bar" {
		t.Fail()
	}
}

func TestConvertNormalPreferences(t *testing.T) {
	record := &UserPreferencesRecord{
		ID:          "test_id",
		Preferences: `{"foo":"bar"}`,
		UserID:      "test_user_id",
	}
	actual, err := convertPrefs(record, false)
	if err != nil {
		t.Fail()
	}
	if _, ok := actual["foo"]; !ok {
		t.Fail()
	}
	if actual["foo"].(string) != "bar" {
		t.Fail()
	}
}

func TestHandleNonUser(t *testing.T) {
	var (
		expectedMsg    = "{\"user\":\"test-user\"}\n"
		expectedStatus = http.StatusBadRequest
	)

	recorder := httptest.NewRecorder()
	handleNonUser(recorder, "test-user")
	actualMsg := recorder.Body.String()
	actualStatus := recorder.Code

	if actualStatus != expectedStatus {
		t.Errorf("Status code was %d but should have been %d", actualStatus, expectedStatus)
	}

	if actualMsg != expectedMsg {
		t.Errorf("Message was '%s' but should have been '%s'", actualMsg, expectedMsg)
	}
}

func TestPreferencesGreeting(t *testing.T) {
	mock := NewMockDB()
	router := mux.NewRouter()
	router.Handle("/preferences/debug/vars", http.DefaultServeMux)
	n := NewPrefsApp(mock, router)

	server := httptest.NewServer(n.router)
	defer server.Close()

	res, err := http.Get(server.URL)
	if err != nil {
		t.Error(err)
	}

	expectedBody := []byte("Hello from user-preferences.\n")
	actualBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	res.Body.Close()

	if !bytes.Equal(actualBody, expectedBody) {
		t.Errorf("Message was '%s' but should have been '%s'", actualBody, expectedBody)
	}

	expectedStatus := http.StatusOK
	actualStatus := res.StatusCode

	if actualStatus != expectedStatus {
		t.Errorf("Status code was %d but should have been %d", actualStatus, expectedStatus)
	}
}

func TestGetUserPreferencesForRequest(t *testing.T) {
	mock := NewMockDB()
	router := mux.NewRouter()
	n := NewPrefsApp(mock, router)

	expected := []byte("{\"one\":\"two\"}")
	expectedWrapped := []byte("{\"preferences\":{\"one\":\"two\"}}")
	mock.users["test-user"] = true
	if err := mock.insertPreferences("test-user", string(expected)); err != nil {
		t.Error(err)
	}

	actualWrapped, err := n.getUserPreferencesForRequest("test-user", true)
	if err != nil {
		t.Error(err)
	}

	if !bytes.Equal(actualWrapped, expectedWrapped) {
		t.Errorf("The return value was '%s' instead of '%s'", actualWrapped, expectedWrapped)
	}

	actual, err := n.getUserPreferencesForRequest("test-user", false)
	if err != nil {
		t.Error(err)
	}

	if !bytes.Equal(actual, expected) {
		t.Errorf("The return value was '%s' instead of '%s'", actual, expected)
	}
}

func TestPreferencesGetRequest(t *testing.T) {
	mock := NewMockDB()
	router := mux.NewRouter()
	n := NewPrefsApp(mock, router)

	expected := []byte("{\"one\":\"two\"}")
	mock.users["test-user"] = true
	if err := mock.insertPreferences("test-user", string(expected)); err != nil {
		t.Error(err)
	}

	server := httptest.NewServer(n.router)
	defer server.Close()

	url := fmt.Sprintf("%s/%s", server.URL, "preferences/test-user")
	res, err := http.Get(url)
	if err != nil {
		t.Error(err)
	}

	actualBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	res.Body.Close()

	if !bytes.Equal(actualBody, expected) {
		t.Errorf("Message was '%s' but should have been '%s'", actualBody, expected)
	}

	expectedStatus := http.StatusOK
	actualStatus := res.StatusCode

	if actualStatus != expectedStatus {
		t.Errorf("Status code was %d but should have been %d", actualStatus, expectedStatus)
	}
}

func TestPreferencesPutRequest(t *testing.T) {
	mock := NewMockDB()
	router := mux.NewRouter()
	n := NewPrefsApp(mock, router)

	username := "test-user"
	expected := []byte(`{"one":"two"}`)

	mock.users[username] = true

	server := httptest.NewServer(n.router)
	defer server.Close()

	url := fmt.Sprintf("%s/%s", server.URL, "preferences/"+username)
	httpClient := &http.Client{}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(expected))
	if err != nil {
		t.Error(err)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		t.Error(err)
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	res.Body.Close()

	var parsed map[string]map[string]string
	if err = json.Unmarshal(body, &parsed); err != nil {
		t.Error(err)
	}

	var expectedParsed map[string]string
	if err = json.Unmarshal(expected, &expectedParsed); err != nil {
		t.Error(err)
	}

	if _, ok := parsed["preferences"]; !ok {
		t.Error("JSON did not contain a 'preferences' key")
	}

	if !reflect.DeepEqual(parsed["preferences"], expectedParsed) {
		t.Errorf("Put returned %#v instead of %#v", parsed["preferences"], expectedParsed)
	}
}

func TestPreferencesPostRequest(t *testing.T) {
	mock := NewMockDB()
	router := mux.NewRouter()
	n := NewPrefsApp(mock, router)

	username := "test-user"
	expected := []byte(`{"one":"two"}`)

	mock.users[username] = true
	if err := mock.insertPreferences(username, string(expected)); err != nil {
		t.Error(err)
	}

	server := httptest.NewServer(n.router)
	defer server.Close()

	url := fmt.Sprintf("%s/%s", server.URL, "preferences/"+username)
	httpClient := &http.Client{}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(expected))
	if err != nil {
		t.Error(err)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		t.Error(err)
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	res.Body.Close()

	var parsed map[string]map[string]string
	if err = json.Unmarshal(body, &parsed); err != nil {
		t.Error(err)
	}

	var expectedParsed map[string]string
	if err = json.Unmarshal(expected, &expectedParsed); err != nil {
		t.Error(err)
	}

	if _, ok := parsed["preferences"]; !ok {
		t.Error("JSON did not contain a 'preferences' key")
	}

	if !reflect.DeepEqual(parsed["preferences"], expectedParsed) {
		t.Errorf("POST requeted %#v instead of %#v", parsed["preferences"], expectedParsed)
	}
}

func TestPreferencesDelete(t *testing.T) {
	username := "test-user"
	expected := []byte(`{"one":"two"}`)

	mock := NewMockDB()
	mock.users[username] = true
	router := mux.NewRouter()
	n := NewPrefsApp(mock, router)

	if err := mock.insertPreferences(username, string(expected)); err != nil {
		t.Error(err)
	}

	server := httptest.NewServer(n.router)
	defer server.Close()

	url := fmt.Sprintf("%s/%s", server.URL, "preferences/"+username)
	httpClient := &http.Client{}
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Error(err)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		t.Error(err)
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	res.Body.Close()

	if len(body) > 0 {
		t.Errorf("DELETE returned a body: %s", body)
	}

	expectedStatus := http.StatusOK
	actualStatus := res.StatusCode

	if actualStatus != expectedStatus {
		t.Errorf("DELETE status code was %d instead of %d", actualStatus, expectedStatus)
	}
}

func TestNewPrefsDB(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error occurred creating the mock db: %s", err)
	}
	defer db.Close()

	prefs := NewPrefsDB(db)
	if prefs == nil {
		t.Error("NewPrefsDB() returned nil")
	}

	if prefs.db != db {
		t.Error("dbs did not match")
	}
}

func TestPreferencesIsUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewPrefsDB(db)
	if p == nil {
		t.Error("NewPrefsDB returned nil")
	}

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM \\( SELECT DISTINCT id FROM users").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"check_user"}).AddRow(1))

	present, err := p.isUser("test-user")
	if err != nil {
		t.Errorf("error calling isUser(): %s", err)
	}

	if !present {
		t.Error("test-user was not found")
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}
}

func TestHasPreferences(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewPrefsDB(db)
	if p == nil {
		t.Error("NewPrefsDB returned nil")
	}

	mock.ExpectQuery("SELECT COUNT\\(p.\\*\\) FROM user_preferences p, users u WHERE p.user_id = u.id").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{""}).AddRow("1"))

	hasPrefs, err := p.hasPreferences("test-user")
	if err != nil {
		t.Errorf("error from hasPreferences(): %s", err)
	}

	if !hasPrefs {
		t.Error("hasPreferences() returned false")
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}
}

func TestGetPreferences(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewPrefsDB(db)
	if p == nil {
		t.Error("NewPrefsDB returned nil")
	}

	mock.ExpectQuery("SELECT p.id AS id, p.user_id AS user_id, p.preferences AS preferences FROM user_preferences p, users u WHERE p.user_id = u.id AND u.username =").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "preferences"}).AddRow("1", "2", "{}"))

	records, err := p.getPreferences("test-user")
	if err != nil {
		t.Errorf("error from getPreferences(): %s", err)
	}

	if len(records) != 1 {
		t.Errorf("number of records returned was %d instead of 1", len(records))
	}

	prefs := records[0]
	if prefs.UserID != "2" {
		t.Errorf("user id was %s instead of 2", prefs.UserID)
	}

	if prefs.ID != "1" {
		t.Errorf("id was %s instead of 1", prefs.ID)
	}

	if prefs.Preferences != "{}" {
		t.Errorf("preferences was %s instead of '{}'", prefs.Preferences)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}
}

func TestInsertPreferences(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewPrefsDB(db)
	if p == nil {
		t.Error("NewPrefsDB returned nil")
	}

	mock.ExpectQuery("SELECT id FROM users WHERE username =").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("1"))

	mock.ExpectExec("INSERT INTO user_preferences \\(user_id, preferences\\) VALUES").
		WithArgs("1", "{}").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err = p.insertPreferences("test-user", "{}"); err != nil {
		t.Errorf("error inserting preferences: %s", err)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}
}

func TestUpdatePreferences(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewPrefsDB(db)
	if p == nil {
		t.Error("NewPrefsDB returned nil")
	}

	mock.ExpectQuery("SELECT id FROM users WHERE username =").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("1"))

	mock.ExpectExec("UPDATE ONLY user_preferences SET preferences =").
		WithArgs("1", "{}").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err = p.updatePreferences("test-user", "{}"); err != nil {
		t.Errorf("error updating preferences: %s", err)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}
}

func TestDeletePreferences(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewPrefsDB(db)
	if p == nil {
		t.Error("NewPrefsDB returned nil")
	}

	mock.ExpectQuery("SELECT id FROM users WHERE username =").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("1"))

	mock.ExpectExec("DELETE FROM ONLY user_preferences WHERE user_id =").
		WithArgs("1").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err = p.deletePreferences("test-user"); err != nil {
		t.Errorf("error deleting preferences: %s", err)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}
}

// -------- End Preferences --------

// -------- Start Sessions --------
func (m *MockDB) hasSessions(username string) (bool, error) {
	stored, ok := m.storage[username]
	if !ok {
		return false, nil
	}
	if stored == nil {
		return false, nil
	}
	prefs, ok := m.storage[username]["user-sessions"].(string)
	if !ok {
		return false, nil
	}
	if prefs == "" {
		return false, nil
	}
	return true, nil
}

func (m *MockDB) getSessions(username string) ([]UserSessionRecord, error) {
	return []UserSessionRecord{
		UserSessionRecord{
			ID:      "id",
			Session: m.storage[username]["user-sessions"].(string),
			UserID:  "user-id",
		},
	}, nil
}

func (m *MockDB) insertSession(username, session string) error {
	if _, ok := m.storage[username]["user-sessions"]; !ok {
		m.storage[username] = make(map[string]interface{})
	}
	m.storage[username]["user-sessions"] = session
	return nil
}

func (m *MockDB) updateSession(username, prefs string) error {
	return m.insertSession(username, prefs)
}

func (m *MockDB) deleteSession(username string) error {
	delete(m.storage, username)
	return nil
}

func TestConvertBlankSession(t *testing.T) {
	record := &UserSessionRecord{
		ID:      "test_id",
		Session: "",
		UserID:  "test_user_id",
	}
	actual, err := convertSessions(record, false)
	if err != nil {
		t.Error(err)
	}
	if len(actual) > 0 {
		t.Fail()
	}
}

func TestConvertUnparseableSession(t *testing.T) {
	record := &UserSessionRecord{
		ID:      "test_id",
		Session: "------------",
		UserID:  "test_user_id",
	}
	actual, err := convertSessions(record, false)
	if err == nil {
		t.Fail()
	}
	if actual != nil {
		t.Fail()
	}
}

func TestConvertEmbeddedSession(t *testing.T) {
	record := &UserSessionRecord{
		ID:      "test_id",
		Session: `{"session":{"foo":"bar"}}`,
		UserID:  "test_user_id",
	}
	actual, err := convertSessions(record, false)
	if err != nil {
		t.Fail()
	}
	if _, ok := actual["foo"]; !ok {
		t.Fail()
	}
	if actual["foo"].(string) != "bar" {
		t.Fail()
	}
}

func TestConvertNormalSession(t *testing.T) {
	record := &UserSessionRecord{
		ID:      "test_id",
		Session: `{"foo":"bar"}`,
		UserID:  "test_user_id",
	}
	actual, err := convertSessions(record, false)
	if err != nil {
		t.Fail()
	}
	if _, ok := actual["foo"]; !ok {
		t.Fail()
	}
	if actual["foo"].(string) != "bar" {
		t.Fail()
	}
}

func TestGetUserSessionForRequest(t *testing.T) {
	mock := NewMockDB()
	router := mux.NewRouter()
	n := NewSessionsApp(mock, router)

	expected := []byte("{\"one\":\"two\"}")
	expectedWrapped := []byte("{\"session\":{\"one\":\"two\"}}")
	mock.users["test-user"] = true
	if err := mock.insertSession("test-user", string(expected)); err != nil {
		t.Error(err)
	}

	actualWrapped, err := n.getUserSessionForRequest("test-user", true)
	if err != nil {
		t.Error(err)
	}

	if !bytes.Equal(actualWrapped, expectedWrapped) {
		t.Errorf("The return value was '%s' instead of '%s'", actualWrapped, expectedWrapped)
	}

	actual, err := n.getUserSessionForRequest("test-user", false)
	if err != nil {
		t.Error(err)
	}

	if !bytes.Equal(actual, expected) {
		t.Errorf("The return value was '%s' instead of '%s'", actual, expected)
	}
}

func TestSessionsGetRequest(t *testing.T) {
	mock := NewMockDB()
	router := mux.NewRouter()
	n := NewSessionsApp(mock, router)

	expected := []byte("{\"one\":\"two\"}")
	mock.users["test-user"] = true
	if err := mock.insertSession("test-user", string(expected)); err != nil {
		t.Error(err)
	}

	server := httptest.NewServer(n.router)
	defer server.Close()

	url := fmt.Sprintf("%s/%s", server.URL, "sessions/test-user")
	res, err := http.Get(url)
	if err != nil {
		t.Error(err)
	}

	actualBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	res.Body.Close()

	if !bytes.Equal(actualBody, expected) {
		t.Errorf("Message was '%s' but should have been '%s'", actualBody, expected)
	}

	expectedStatus := http.StatusOK
	actualStatus := res.StatusCode

	if actualStatus != expectedStatus {
		t.Errorf("Status code was %d but should have been %d", actualStatus, expectedStatus)
	}
}

func TestSessionsPutRequest(t *testing.T) {
	mock := NewMockDB()
	router := mux.NewRouter()
	n := NewSessionsApp(mock, router)

	username := "test-user"
	expected := []byte(`{"one":"two"}`)

	mock.users[username] = true

	server := httptest.NewServer(n.router)
	defer server.Close()

	url := fmt.Sprintf("%s/%s", server.URL, "sessions/"+username)
	httpClient := &http.Client{}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(expected))
	if err != nil {
		t.Error(err)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		t.Error(err)
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	res.Body.Close()

	var parsed map[string]map[string]string
	if err = json.Unmarshal(body, &parsed); err != nil {
		t.Error(err)
	}

	var expectedParsed map[string]string
	if err = json.Unmarshal(expected, &expectedParsed); err != nil {
		t.Error(err)
	}

	if _, ok := parsed["session"]; !ok {
		t.Error("JSON did not contain a 'preferences' key")
	}

	if !reflect.DeepEqual(parsed["session"], expectedParsed) {
		t.Errorf("Put returned %#v instead of %#v", parsed["session"], expectedParsed)
	}
}

func TestSessionsPostRequest(t *testing.T) {
	mock := NewMockDB()
	router := mux.NewRouter()
	n := NewSessionsApp(mock, router)

	username := "test-user"
	expected := []byte(`{"one":"two"}`)

	mock.users[username] = true
	if err := mock.insertSession(username, string(expected)); err != nil {
		t.Error(err)
	}

	server := httptest.NewServer(n.router)
	defer server.Close()

	url := fmt.Sprintf("%s/%s", server.URL, "sessions/"+username)
	httpClient := &http.Client{}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(expected))
	if err != nil {
		t.Error(err)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		t.Error(err)
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	res.Body.Close()

	var parsed map[string]map[string]string
	if err = json.Unmarshal(body, &parsed); err != nil {
		t.Error(err)
	}

	var expectedParsed map[string]string
	if err = json.Unmarshal(expected, &expectedParsed); err != nil {
		t.Error(err)
	}

	if _, ok := parsed["session"]; !ok {
		t.Error("JSON did not contain a 'preferences' key")
	}

	if !reflect.DeepEqual(parsed["session"], expectedParsed) {
		t.Errorf("POST requeted %#v instead of %#v", parsed["session"], expectedParsed)
	}
}

func TestSessionsDelete(t *testing.T) {
	username := "test-user"
	expected := []byte(`{"one":"two"}`)

	mock := NewMockDB()
	mock.users[username] = true
	router := mux.NewRouter()
	n := NewSessionsApp(mock, router)

	if err := mock.insertSession(username, string(expected)); err != nil {
		t.Error(err)
	}

	server := httptest.NewServer(n.router)
	defer server.Close()

	url := fmt.Sprintf("%s/%s", server.URL, "sessions/"+username)
	httpClient := &http.Client{}
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Error(err)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		t.Error(err)
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	res.Body.Close()

	if len(body) > 0 {
		t.Errorf("DELETE returned a body: %s", body)
	}

	expectedStatus := http.StatusOK
	actualStatus := res.StatusCode

	if actualStatus != expectedStatus {
		t.Errorf("DELETE status code was %d instead of %d", actualStatus, expectedStatus)
	}
}

func TestNewSessionsDB(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewSessionsDB(db)
	if p == nil {
		t.Error("NewSessionsDB returned nil")
	}

	if db != p.db {
		t.Error("dbs did not match")
	}
}

func TestSessionsIsUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewSessionsDB(db)
	if p == nil {
		t.Error("NewSessionsDB returned nil")
	}

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM \\( SELECT DISTINCT id FROM users").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"check_user"}).AddRow(1))

	present, err := p.isUser("test-user")
	if err != nil {
		t.Errorf("error calling isUser(): %s", err)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}

	if !present {
		t.Error("test-user was not found")
	}
}

func TestHasSessions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewSessionsDB(db)
	if p == nil {
		t.Error("NewSessionsDB returned nil")
	}

	mock.ExpectQuery("SELECT COUNT\\(s.\\*\\) FROM user_sessions s, users u WHERE s.user_id = u.id").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{""}).AddRow("1"))

	hasSessions, err := p.hasSessions("test-user")
	if err != nil {
		t.Errorf("error from hasSessions(): %s", err)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}

	if !hasSessions {
		t.Error("hasSessions() returned false")
	}
}

func TestGetSessions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewSessionsDB(db)
	if p == nil {
		t.Error("NewSessionsDB returned nil")
	}

	mock.ExpectQuery("SELECT s.id AS id, s.user_id AS user_id, s.session AS session FROM user_sessions s, users u WHERE s.user_id = u.id AND u.username =").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "session"}).AddRow("1", "2", "{}"))

	records, err := p.getSessions("test-user")
	if err != nil {
		t.Errorf("error from getSessions(): %s", err)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}

	if len(records) != 1 {
		t.Errorf("number of records returned was %d instead of 1", len(records))
	}

	session := records[0]
	if session.UserID != "2" {
		t.Errorf("user id was %s instead of 2", session.UserID)
	}

	if session.ID != "1" {
		t.Errorf("id was %s instead of 1", session.ID)
	}

	if session.Session != "{}" {
		t.Errorf("session was %s instead of '{}'", session.Session)
	}
}

func TestInsertSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewSessionsDB(db)
	if p == nil {
		t.Error("NewSessionsDB returned nil")
	}

	mock.ExpectQuery("SELECT id FROM users WHERE username =").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("1"))

	mock.ExpectExec("INSERT INTO user_sessions \\(user_id, session\\) VALUES").
		WithArgs("1", "{}").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err = p.insertSession("test-user", "{}"); err != nil {
		t.Errorf("error inserting session: %s", err)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}
}

func TestUpdateSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewSessionsDB(db)
	if p == nil {
		t.Error("NewSessionsDB returned nil")
	}

	mock.ExpectQuery("SELECT id FROM users WHERE username =").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("1"))

	mock.ExpectExec("UPDATE ONLY user_sessions SET session =").
		WithArgs("1", "{}").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err = p.updateSession("test-user", "{}"); err != nil {
		t.Errorf("error updating session: %s", err)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}
}

func TestDeleteSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating the mock db: %s", err)
	}
	defer db.Close()

	p := NewSessionsDB(db)
	if p == nil {
		t.Error("NewSessionsDB returned nil")
	}

	mock.ExpectQuery("SELECT id FROM users WHERE username =").
		WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("1"))

	mock.ExpectExec("DELETE FROM ONLY user_sessions WHERE user_id =").
		WithArgs("1").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err = p.deleteSession("test-user"); err != nil {
		t.Errorf("error deleting session: %s", err)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations were not met: %s", err)
	}
}

// -------- End Sessions --------

func TestFixAddrNoPrefix(t *testing.T) {
	expected := ":70000"
	actual := fixAddr("70000")
	if actual != expected {
		t.Fail()
	}
}

func TestFixAddrWithPrefix(t *testing.T) {
	expected := ":70000"
	actual := fixAddr(":70000")
	if actual != expected {
		t.Fail()
	}
}

func TestBadRequest(t *testing.T) {
	var (
		expectedMsg    = "test message\n"
		expectedStatus = http.StatusBadRequest
	)

	recorder := httptest.NewRecorder()
	badRequest(recorder, "test message")
	actualMsg := recorder.Body.String()
	actualStatus := recorder.Code

	if actualStatus != expectedStatus {
		t.Errorf("Status code was %d but should have been %d", actualStatus, expectedStatus)
	}

	if actualMsg != expectedMsg {
		t.Errorf("Message was '%s' but should have been '%s'", actualMsg, expectedMsg)
	}
}

func TestErrored(t *testing.T) {
	var (
		expectedMsg    = "test message\n"
		expectedStatus = http.StatusInternalServerError
	)

	recorder := httptest.NewRecorder()
	errored(recorder, "test message")
	actualMsg := recorder.Body.String()
	actualStatus := recorder.Code

	if actualStatus != expectedStatus {
		t.Errorf("Status code was %d but should have been %d", actualStatus, expectedStatus)
	}

	if actualMsg != expectedMsg {
		t.Errorf("Message was '%s' but should have been '%s'", actualMsg, expectedMsg)
	}
}

func TestDeleteUnstored(t *testing.T) {
	username := "test-user"
	mock := NewMockDB()
	mock.users[username] = true
	router := mux.NewRouter()
	np := NewPrefsApp(mock, router)
	ns := NewSessionsApp(mock, router)

	serverPrefs := httptest.NewServer(np.router)
	serverSessions := httptest.NewServer(ns.router)
	defer serverPrefs.Close()
	defer serverSessions.Close()

	urlPrefs := fmt.Sprintf("%s/%s", serverPrefs.URL, "preferences/"+username)
	urlSessions := fmt.Sprintf("%s/%s", serverSessions.URL, "sessions/"+username)
	httpClient := &http.Client{}
	reqPrefs, errPrefs := http.NewRequest(http.MethodDelete, urlPrefs, nil)
	if errPrefs != nil {
		t.Error(errPrefs)
	}
	reqSessions, errSessions := http.NewRequest(http.MethodDelete, urlSessions, nil)
	if errSessions != nil {
		t.Error(errSessions)
	}

	resPrefs, errPrefs := httpClient.Do(reqPrefs)
	if errPrefs != nil {
		t.Error(errPrefs)
	}
	resSessions, errSessions := httpClient.Do(reqSessions)
	if errSessions != nil {
		t.Error(errSessions)
	}

	bodyPrefs, errPrefs := ioutil.ReadAll(resPrefs.Body)
	if errPrefs != nil {
		t.Error(errPrefs)
	}
	resPrefs.Body.Close()

	bodySessions, errSessions := ioutil.ReadAll(resSessions.Body)
	if errSessions != nil {
		t.Error(errSessions)
	}
	resSessions.Body.Close()

	if len(bodyPrefs) > 0 {
		t.Errorf("DELETE returned a body: %s", bodyPrefs)
	}
	if len(bodySessions) > 0 {
		t.Errorf("DELETE returned a body: %s", bodySessions)
	}

	expectedStatus := http.StatusOK
	actualStatusPrefs := resPrefs.StatusCode
	actualStatusSessions := resSessions.StatusCode

	if actualStatusPrefs != expectedStatus {
		t.Errorf("DELETE status code was %d instead of %d", actualStatusPrefs, expectedStatus)
	}
	if actualStatusSessions != expectedStatus {
		t.Errorf("DELETE status code was %d instead of %d", actualStatusSessions, expectedStatus)
	}
}
