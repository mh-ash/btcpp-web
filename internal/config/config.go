package config

import (
	"html/template"
	"log"

	"github.com/alexedwards/scs/v2"
	"github.com/base58btc/btcpp-web/internal/types"
)

// AppConfig holds the application configuration settings
type AppConfig struct {
	InProduction  bool
	InfoLog       *log.Logger
	ErrorLog      *log.Logger
	Session       *scs.SessionManager
	TemplateCache map[string]*template.Template

	Context types.AppContext
}
