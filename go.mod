module github.com/base58btc/btcpp-web

go 1.20

require (
	github.com/BurntSushi/toml v1.2.1
	github.com/alexedwards/scs/v2 v2.5.1
	github.com/gorilla/mux v1.8.0
	github.com/profclems/go-dotenv v0.1.2
	github.com/sendgrid/sendgrid-go v3.12.0+incompatible
	github.com/sorcererxw/go-notion v0.2.4
)

require (
	github.com/google/renameio v0.1.0 // indirect
	github.com/sendgrid/rest v2.6.9+incompatible // indirect
	github.com/spf13/cast v1.3.1 // indirect
	golang.org/x/net v0.8.0 // indirect
)

replace github.com/sorcererxw/go-notion v0.2.4 => github.com/niftynei/go-notion v0.0.0-20230323155332-a2c93bab119e
