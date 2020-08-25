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

// BagsApp contains the routing and request handling code for bags.
type BagsApp struct {
	api    *BagsAPI
	router *mux.Router
}

// NewBagsApp creates a new BagsApp instance.
func NewBagsApp(db *sql.DB, router *mux.Router) *BagsApp {
	bagsApp := &BagsApp{
		api: &BagsAPI{
			db: db,
		},
		router: router,
	}
	bagsApp.router.HandleFunc("/bags/", bagsApp.Greeting).Methods(http.MethodGet)
	bagsApp.router.HandleFunc("/bags/{username}", bagsApp.GetBags).Methods(http.MethodGet)
	bagsApp.router.HandleFunc("/bags/{username}/{bagID}", bagsApp.GetBag).Methods(http.MethodGet)
	bagsApp.router.HandleFunc("/bags/{username", bagsApp.AddBag).Methods(http.MethodPut)
	return bagsApp
}

// Greeting prints out a greeting for the bags endpoints.
func (b *BagsApp) Greeting(writer http.ResponseWriter, request *http.Request) {
	fmt.Fprintf(writer, "Hello from the bags handler")
}

// GetBags returns a listing of the bags for the user.
func (b *BagsApp) GetBags(writer http.ResponseWriter, request *http.Request) {
	var (
		username   string
		bags       []BagRecord
		err        error
		ok         bool
		userExists bool
		vars       = mux.Vars(request)
	)

	if username, ok = vars["username"]; !ok {
		badRequest(writer, "Missing username in the URL")
		return
	}

	if userExists, err = queries.IsUser(b.api.db, username); err != nil {
		badRequest(writer, fmt.Sprintf("error checking for bags %s: %s", username, err))
		return
	}

	if !userExists {
		http.Error(writer, fmt.Sprintf("user %s does not exist", username), http.StatusBadRequest)
		return
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
		ok, userExists  bool
		vars            = mux.Vars(request)
	)

	if username, ok = vars["username"]; !ok {
		badRequest(writer, "missing username in the URL")
		return
	}

	if bagID, ok = vars["bagID"]; !ok {
		badRequest(writer, "missing bagID in the URL")
		return
	}

	if userExists, err = queries.IsUser(b.api.db, username); err != nil {
		badRequest(writer, fmt.Sprintf("error checking for bags %s: %s", username, err))
		return
	}

	if !userExists {
		http.Error(writer, fmt.Sprintf("user %s does not exist", username), http.StatusBadRequest)
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
		ok, userExists  bool
		body            []byte
		retval          []byte
		vars            = mux.Vars(request)
	)

	if username, ok = vars["username"]; !ok {
		badRequest(writer, "missing username in the URL")
		return
	}

	if userExists, err = queries.IsUser(b.api.db, username); err != nil {
		badRequest(writer, fmt.Sprintf("error checking for bags %s: %s", username, err))
		return
	}

	if !userExists {
		badRequest(writer, fmt.Sprintf("user %s does not exist", username))
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
