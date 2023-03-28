package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/alexedwards/scs/v2"
	"github.com/base58btc/btcpp-web/internal/config"
	"github.com/base58btc/btcpp-web/internal/handlers"
	"github.com/base58btc/btcpp-web/internal/types"
)

const configFile = "config.toml"

var app config.AppContext

func loadConfig() *types.EnvConfig {
	var config types.EnvConfig

	if _, err := os.Stat("config.toml"); err == nil {
		_, err = toml.DecodeFile(configFile, &config)
		if err != nil {
			log.Fatal(err)
		}
		config.Prod = false
	} else {
		config.Port = os.Getenv("PORT")
		config.Prod = true

		config.RegistryPin = os.Getenv("REGISTRY_PIN")
		config.Notion = types.NotionConfig{
			Token: os.Getenv("NOTION_TOKEN"),
			TalksDb: os.Getenv("NOTION_TALKS_DB"),
			PurchasesDb: os.Getenv("NOTION_PURCHASES_DB"),
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
	routes, err := handlers.Routes(&app)
	if err != nil {
		app.Err.Fatal(err)
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", app.Env.Port),
		Handler: app.Session.LoadAndSave(routes),
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
	var logfile *os.File
	var err error
	if env.LogFile != "" {
		fmt.Println("Using logfile:", env.LogFile)
		logfile, err = os.OpenFile(env.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			return err
		}
	} else {
		fmt.Println("Using logfile: stdout")
		logfile = os.Stdout
	}

	app.Infos = log.New(logfile, "INFO\t", log.Ldate|log.Ltime)
	app.Err = log.New(logfile, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	// Initialize the application configuration
	app.InProduction = env.Prod

	app.Infos.Println("\n\n\n")
	app.Infos.Println("~~~~app restarted, here we go~~~~~")
	app.Infos.Println("Running in prod?", env.Prod)

	// Initialize the session manager
	app.Session = scs.New()
	app.Session.Lifetime = 4 * 24 * time.Hour
	app.Session.Cookie.Persist = true
	app.Session.Cookie.SameSite = http.SameSiteLaxMode
	app.Session.Cookie.Secure = app.InProduction

	app.Notion = &types.Notion{Config: env.Notion}
	app.Notion.Setup()

	return nil
}
