package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/cyverse-de/queries"
	"github.com/gorilla/mux"
)

// BagsApp contains the routing and request handling code for bags.
type BagsApp struct {
	api        *BagsAPI
	router     *mux.Router
	userDomain string
}

// NewBagsApp creates a new BagsApp instance.
func NewBagsApp(db *sql.DB, router *mux.Router, userDomain string) *BagsApp {
	bagsApp := &BagsApp{
		api: &BagsAPI{
			db: db,
		},
		router:     router,
		userDomain: userDomain,
	}
	bagsApp.router.HandleFunc("/bags/", bagsApp.Greeting).Methods(http.MethodGet)
	bagsApp.router.HandleFunc("/bags/{username}", bagsApp.HasBags).Methods(http.MethodHead)
	bagsApp.router.HandleFunc("/bags/{username}", bagsApp.GetBags).Methods(http.MethodGet)
	bagsApp.router.HandleFunc("/bags/{username}/{bagID}", bagsApp.GetBag).Methods(http.MethodGet)
	bagsApp.router.HandleFunc("/bags/{username}", bagsApp.AddBag).Methods(http.MethodPut)
	bagsApp.router.HandleFunc("/bags/{username}/{bagID}", bagsApp.UpdateBag).Methods(http.MethodPost)
	bagsApp.router.HandleFunc("/bags/{username}/{bagID}", bagsApp.DeleteBag).Methods(http.MethodDelete)
	bagsApp.router.HandleFunc("/bags/{username}", bagsApp.DeleteAllBags).Methods(http.MethodDelete)
	return bagsApp
}

// AddUsernameSuffix appends the @iplantcollaborative.org string to the
// username if it's not already there.
func (b *BagsApp) AddUsernameSuffix(username string) string {
	var retval string
	if !strings.HasSuffix(username, IplantSuffix) {
		retval = fmt.Sprintf("%s%s", username, IplantSuffix)
	} else {
		retval = username
	}
	return retval
}

// Greeting prints out a greeting for the bags endpoints.
func (b *BagsApp) Greeting(writer http.ResponseWriter, request *http.Request) {
	fmt.Fprintf(writer, "Hello from the bags handler")
}

func (b *BagsApp) getUser(vars map[string]string) (string, error) {
	var (
		username       string
		err            error
		ok, userExists bool
	)
	if username, ok = vars["username"]; !ok {
		return "", errors.New("missing username in the URL")
	}

	username = b.AddUsernameSuffix(username)

	if userExists, err = queries.IsUser(b.api.db, username); err != nil {
		return "", fmt.Errorf("error checking for bags %s: %s", username, err)
	}

	if !userExists {
		return "", fmt.Errorf("user %s does not exist", username)
	}

	return username, nil
}

// GetBags returns a listing of the bags for the user.
func (b *BagsApp) GetBags(writer http.ResponseWriter, request *http.Request) {
	var (
		username string
		bags     []BagRecord
		err      error
		vars     = mux.Vars(request)
	)

	if username, err = b.getUser(vars); err != nil {
		badRequest(writer, err.Error())
	}

	if bags, err = b.api.GetBags(username); err != nil {
		http.Error(writer, fmt.Sprintf("error getting bags for %s: %s", username, err), http.StatusInternalServerError)
		return
	}

	jsonBytes, err := json.Marshal(map[string][]BagRecord{"bags": bags})
	if err != nil {
		http.Error(writer, fmt.Sprintf("error JSON encoding result for %s: %s", username, err), http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Write(jsonBytes)
}

// GetBag returns a single bag.
func (b *BagsApp) GetBag(writer http.ResponseWriter, request *http.Request) {
	var (
		username, bagID string
		bag             BagRecord
		err             error
		ok              bool
		vars            = mux.Vars(request)
	)

	if username, err = b.getUser(vars); err != nil {
		badRequest(writer, err.Error())
	}

	if bagID, ok = vars["bagID"]; !ok {
		badRequest(writer, "missing bagID in the URL")
		return
	}

	if bag, err = b.api.GetBag(username, bagID); err != nil {
		http.Error(writer, fmt.Sprintf("error getting bags for %s: %s", username, err), http.StatusInternalServerError)
		return
	}

	jsonBytes, err := json.Marshal(bag)
	if err != nil {
		http.Error(writer, fmt.Sprintf("error JSON encoding result for %s: %s", username, err), http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Write(jsonBytes)
}

// AddBag adds an additional bag to the list for the user.
func (b *BagsApp) AddBag(writer http.ResponseWriter, request *http.Request) {
	var (
		username, bagID string
		bag             BagRecord
		err             error
		body            []byte
		retval          []byte
		vars            = mux.Vars(request)
	)

	if username, err = b.getUser(vars); err != nil {
		badRequest(writer, err.Error())
	}

	if body, err = ioutil.ReadAll(request.Body); err != nil {
		errored(writer, fmt.Sprintf("error reading body: %s", err))
		return
	}

	if err = json.Unmarshal(body, &bag); err != nil {
		badRequest(writer, fmt.Sprintf("failed to JSON decode body: %s", err))
		return
	}

	if bagID, err = b.api.AddBag(username, string(body)); err != nil {
		errored(writer, fmt.Sprintf("failed to add bag for %s: %s", username, err))
		return
	}

	if retval, err = json.Marshal(map[string]string{"id": bagID}); err != nil {
		errored(writer, fmt.Sprintf("failed to JSON encode response body: %s", err))
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Write(retval)
}

// UpdateBag updates the indicated bag.
func (b *BagsApp) UpdateBag(writer http.ResponseWriter, request *http.Request) {
	var (
		username, bagID string
		bag             BagRecord
		err             error
		ok              bool
		body            []byte
		vars            = mux.Vars(request)
	)

	if username, err = b.getUser(vars); err != nil {
		badRequest(writer, err.Error())
	}

	if bagID, ok = vars["bagID"]; !ok {
		badRequest(writer, "missing bagID in the URL")
		return
	}

	if body, err = ioutil.ReadAll(request.Body); err != nil {
		errored(writer, fmt.Sprintf("error reading body: %s", err))
		return
	}

	if err = json.Unmarshal(body, &bag); err != nil {
		errored(writer, fmt.Sprintf("failed to JSON decode body: %s", err))
		return
	}

	if err = b.api.UpdateBag(username, bagID, string(body)); err != nil {
		errored(writer, fmt.Sprintf("error updating bag for user %s: %s", username, err))
		return
	}
}

// DeleteBag deletes a single bag for a user.
func (b *BagsApp) DeleteBag(writer http.ResponseWriter, request *http.Request) {
	var (
		username, bagID string
		err             error
		ok              bool
		vars            = mux.Vars(request)
	)

	if username, err = b.getUser(vars); err != nil {
		badRequest(writer, err.Error())
	}

	if bagID, ok = vars["bagID"]; !ok {
		badRequest(writer, "missing bagID in the URL")
		return
	}

	if err = b.api.DeleteBag(username, bagID); err != nil {
		errored(writer, fmt.Sprintf("error deleting bag for user %s: %s", username, err))
		return
	}
}

// DeleteAllBags deletes all bags for a user
func (b *BagsApp) DeleteAllBags(writer http.ResponseWriter, request *http.Request) {
	var (
		username string
		err      error
		vars     = mux.Vars(request)
	)

	if username, err = b.getUser(vars); err != nil {
		badRequest(writer, err.Error())
	}

	if err = b.api.DeleteAllBags(username); err != nil {
		errored(writer, fmt.Sprintf("error deleting bag for user %s: %s", username, err))
		return
	}
}

// HasBags returns true if the user has at least a single bag in the database.
func (b *BagsApp) HasBags(writer http.ResponseWriter, request *http.Request) {
	var (
		username string
		err      error
		hasBags  bool
		vars     = mux.Vars(request)
	)

	if username, err = b.getUser(vars); err != nil {
		badRequest(writer, err.Error())
	}

	if hasBags, err = b.api.HasBags(username); err != nil {
		errored(writer, fmt.Sprintf("error looking for bags for %s: %s", username, err))
		return
	}

	if !hasBags {
		writer.WriteHeader(http.StatusNotFound)
	} else {
		writer.WriteHeader(http.StatusOK)
	}
}
