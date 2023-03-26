This is the code for the bitcoin++ website. (Conference [Twitter](https://twitter.com/btcplusplus))

All of the configuration values live in a `config.toml` file, which is missing from this repo on purpose.


## To run

make run

## To develop

make dev-run

Note that the project uses @tailwindcss; `make dev-run` will start the server and the process that watches for changes to template files.


## To build 

make build


This will put all the files necessary to serve the site into `target/`
