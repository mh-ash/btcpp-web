package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/mux"
	"github.com/base58btc/btcpp-web/internal/config"
	"github.com/base58btc/btcpp-web/internal/handlers"
	"github.com/base58btc/btcpp-web/internal/types"
)

const configFile = "config.toml"

var app config.AppConfig
var session *scs.SessionManager

func loadConfig() *types.EnvConfig {
	var config types.EnvConfig

	_, err := toml.DecodeFile(configFile, &config)
	if err != nil {
		log.Fatal(err)
	}
	return &config
}

func main() {
	// Initialize the application configuration
	app.InProduction = false // change to true in prod
	app.InfoLog = log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime)
	app.ErrorLog = log.New(os.Stderr, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	// Load configs from config.toml
	env := loadConfig()

	fmt.Println("On port", env.Port)
	// Start the server
	srv := &http.Server{
		Addr:    env.Port,
		Handler: Routes(),
	}

	fmt.Printf("Starting application on port %s\n", env.Port)
	err := run(env)
	if err != nil {
		log.Fatal(err)
	}
	err = srv.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}
}

func run(env *types.EnvConfig) error {
	// Initialize the application configuration
	app.InProduction = false // change to true in production
	app.InfoLog = log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime)
	app.ErrorLog = log.New(os.Stderr, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	// Initialize the session manager
	session = scs.New()
	session.Lifetime = 72 * time.Hour
	session.Cookie.Persist = true
	session.Cookie.SameSite = http.SameSiteLaxMode
	session.Cookie.Secure = app.InProduction

	app.Session = session

	notion := &types.Notion{Config: env.Notion}
	notion.Setup()

	app.Context = types.AppContext{
		Env:    env,
		Notion: notion,
	}
	return nil
}

func getFaviconHandler(name string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, fmt.Sprintf("static/favicon/%s", name))
	}
}

func addFaviconRoutes(r *mux.Router) {
	files, err := ioutil.ReadDir("static/favicon/")
	if err != nil {
		log.Fatal(err)
	}

	/* If asked for a favicon, we'll serve it up */
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		r.HandleFunc(fmt.Sprintf("/%s", file.Name()), getFaviconHandler(file.Name())).Methods("GET")
	}
}

// Routes sets up the routes for the application
func Routes() http.Handler {
	// Create a file server to serve static files from the "static" directory
	fs := http.FileServer(http.Dir("static"))

	r := mux.NewRouter()

	// Set up the routes, we'll have one page per course
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handlers.Home(w, r, &app.Context)
	}).Methods("GET")
	r.HandleFunc("/talks", func(w http.ResponseWriter, r *http.Request) {
		handlers.Talks(w, r, &app.Context)
	}).Methods("GET")
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))
	addFaviconRoutes(r)

	return r
}
