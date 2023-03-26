package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/profclems/go-dotenv"

	"github.com/BurntSushi/toml"
	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/mux"
	"github.com/base58btc/btcpp-web/internal/config"
	"github.com/base58btc/btcpp-web/internal/handlers"
	"github.com/base58btc/btcpp-web/internal/types"
)

const configFile = "config.toml"

var app config.AppContext
var session *scs.SessionManager

func loadConfig() *types.EnvConfig {
	var config types.EnvConfig

	if _, err := os.Stat("config.toml"); err == nil {
		_, err = toml.DecodeFile(configFile, &config)
		if err != nil {
			log.Fatal(err)
		}
		config.Prod = false
	} else if _, err := os.Stat(".env"); err == nil {
		err = dotenv.LoadConfig()
		if err != nil {
			log.Fatal(err)
		}

		/* Populate env */
		config.Port = dotenv.GetString("PORT")
		config.Prod = false
		config.LogFile = dotenv.GetString("LOGFILE")

		config.Notion = types.NotionConfig{
			Token: dotenv.GetString("NOTION_TOKEN"),
			TalksDb: dotenv.GetString("NOTION_TALKS_DB"),
		}
		config.SendGrid = types.SendGridConfig{ Key: dotenv.GetString("SENDGRID_KEY") }
		config.Google = types.GoogleConfig{ Key: dotenv.GetString("GOOGLE_KEY") }

	} else {
		config.Port = os.Getenv("PORT")
		config.Prod = true
		config.LogFile = os.Getenv("LOGFILE")

		config.Notion = types.NotionConfig{
			Token: os.Getenv("NOTION_TOKEN"),
			TalksDb: os.Getenv("NOTION_TALKS_DB"),
		}
		config.SendGrid = types.SendGridConfig{ Key: os.Getenv("SENDGRID_KEY") }
		config.Google = types.GoogleConfig{ Key: os.Getenv("GOOGLE_KEY") }
	}


	return &config
}

func main() {
	/* Load configs from config.toml */
	app.Env = loadConfig()
	err := run(app.Env)
	if err != nil {
		log.Fatal(err)
	}

	/* Set up Routes */
	routes, err := Routes()
	if err != nil {
		app.Err.Fatal(err)
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", app.Env.Port),
		Handler: routes,
	}

	/* Start the server */
	app.Infos.Printf("Starting application on port %s\n", app.Env.Port)
	err = srv.ListenAndServe()
	if err != nil {
		app.Err.Fatal(err)
	}
}

func run(env *types.EnvConfig) error {
	/* Load up the logfile */
	fmt.Println("Using logfile:", env.LogFile)
	logfile, err := os.OpenFile(env.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	app.Infos = log.New(logfile, "INFO\t", log.Ldate|log.Ltime)
	app.Err = log.New(logfile, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	// Initialize the application configuration
	app.InProduction = env.Prod

	app.Infos.Println("\n\n\n")
	app.Infos.Println("~~~~app restarted, here we go~~~~~")
	app.Infos.Println("Running in prod?", env.Prod)

	// Initialize the session manager
	session = scs.New()
	session.Lifetime = 72 * time.Hour
	session.Cookie.Persist = true
	session.Cookie.SameSite = http.SameSiteLaxMode
	session.Cookie.Secure = app.InProduction

	app.Session = session

	app.Notion = &types.Notion{Config: env.Notion}
	app.Notion.Setup()

	return nil
}

func getFaviconHandler(name string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, fmt.Sprintf("static/favicon/%s", name))
	}
}

func addFaviconRoutes(r *mux.Router) error {
	files, err := ioutil.ReadDir("static/favicon/")
	if err != nil {
		return err
	}

	/* If asked for a favicon, we'll serve it up */
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		r.HandleFunc(fmt.Sprintf("/%s", file.Name()), getFaviconHandler(file.Name())).Methods("GET")
	}

	return nil
}

// Routes sets up the routes for the application
func Routes() (http.Handler, error) {
	// Create a file server to serve static files from the "static" directory
	fs := http.FileServer(http.Dir("static"))

	r := mux.NewRouter()

	// Set up the routes, we'll have one page per course
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handlers.Home(w, r, &app)
	}).Methods("GET")
	r.HandleFunc("/talks", func(w http.ResponseWriter, r *http.Request) {
		handlers.Talks(w, r, &app)
	}).Methods("GET")
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))
	err := addFaviconRoutes(r)

	return r, err
}
