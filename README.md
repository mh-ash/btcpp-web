This is the code for the bitcoin++ website. (Conference [Twitter](https://twitter.com/btcplusplus))

All of the configuration values live in a `config.toml` file, which is missing from this repo on purpose.


## Setup Dependencies

We use nix for this. Installs go + tailwindcss dependencies for Makefile.

```
	nix develop
```


## To run

make run

## To develop

make dev-run

Note that the project uses @tailwindcss; `make dev-run` will start the server and the process that watches for changes to template files.

This is janky though; you'll need to `pkill btcpp-web` before restarting...


## To build

make build


This will put all the files necessary to serve the site into `target/`


## Deploy Testing

Currently, we deploy the app using Digital Ocean, using the `Dockerfile`. Sometimes it's useful to test building changes locally. For this, I'd recommend using the `doctl` app.

Instructions [here](https://docs.digitalocean.com/products/app-platform/how-to/build-locally/), but in brief.

```
doctl app dev build
```

Then follow the instructions to run.

FIXME: currently it picks up the config.toml file; remove this and use .env.


Let's add an example fixme!
