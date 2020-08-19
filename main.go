package main

import (
	"encoding/json"
	_ "expvar"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/cyverse-de/configurate"
	"github.com/cyverse-de/dbutil"
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

// -------- START SEARCHES --------

//-------- END SEARCHES DATA --------

func badRequest(writer http.ResponseWriter, msg string) {
	http.Error(writer, msg, http.StatusBadRequest)
	log.Error(msg)
}

func errored(writer http.ResponseWriter, msg string) {
	http.Error(writer, msg, http.StatusInternalServerError)
	log.Error(msg)
}

func notFound(writer http.ResponseWriter, msg string) {
	http.Error(writer, msg, http.StatusNotFound)
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

	notFound(writer, string(retval))

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

func makeRouter() *mux.Router {
	router := mux.NewRouter()
	router.Handle("/debug/vars", http.DefaultServeMux)
	router.HandleFunc("/", func(writer http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(writer, "Hello from user-info.\n")
	}).Methods("GET")

	return router
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

	router := makeRouter()

	log.Info("Listening on port ", *port)
	prefsDB := NewPrefsDB(db)
	prefsApp := NewPrefsApp(prefsDB, router)

	sessionsDB := NewSessionsDB(db)
	sessionsApp := NewSessionsApp(sessionsDB, router)

	searchesDB := NewSearchesDB(db)
	searchesApp := NewSearchesApp(searchesDB, router)

	log.Debug(prefsApp)
	log.Debug(sessionsApp)
	log.Debug(searchesApp)
	log.Fatal(http.ListenAndServe(fixAddr(*port), router))
}
