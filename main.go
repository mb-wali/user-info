package main

import (
	_ "expvar"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/cyverse-de/configurate"
	"github.com/cyverse-de/dbutil"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// IplantSuffix is what is appended to a username in the database.
const IplantSuffix = "@iplantcollaborative.org"

// AddUsernameSuffix appends the @iplantcollaborative.org string to the
// username if it's not already there.
func AddUsernameSuffix(username string) string {
	var retval string
	if !strings.HasSuffix(username, IplantSuffix) {
		retval = fmt.Sprintf("%s%s", username, IplantSuffix)
	} else {
		retval = username
	}
	return retval
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

	prefsDB := NewPrefsDB(db)
	prefsApp := NewPrefsApp(prefsDB, router)

	sessionsDB := NewSessionsDB(db)
	sessionsApp := NewSessionsApp(sessionsDB, router)

	searchesDB := NewSearchesDB(db)
	searchesApp := NewSearchesApp(searchesDB, router)

	bagsApp := NewBagsApp(db, router)

	log.Debug(prefsApp)
	log.Debug(sessionsApp)
	log.Debug(searchesApp)
	log.Debug(bagsApp)

	log.Info("Listening on port ", *port)
	log.Fatal(http.ListenAndServe(fixAddr(*port), router))
}
