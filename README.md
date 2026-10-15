# Plexivo CLI

Connect to your Plexivo databases and caches from your own machine — public or private — through a short-lived, encrypted tunnel.

## Install

macOS and Linux:

```sh
curl -fsSL https://github.com/plexivo/plexivo-cli/releases/latest/download/install.sh | sh
```

Windows: download `plexivo-windows-amd64.zip` from the [latest release](https://github.com/plexivo/plexivo-cli/releases/latest) and put `plexivo.exe` on your `PATH`.

## Sign in

Create an API key in the console (**Settings → API keys**), then:

```sh
plexivo login
```

The key is stored in your user configuration directory, readable only by you. `PLEXIVO_TOKEN` overrides it, for CI.

## Connect to a database

```sh
plexivo db list
plexivo db connect orders-db
```

`plexivo db connect` opens a local port — 5432 for Postgres, 3306 for MySQL, 6380 for Cache, or a free one — and prints the connection string to use with it. Point `psql`, TablePlus, DBeaver, MySQL Workbench or `redis-cli` at it. The tunnel works whether the database is public, limited to trusted networks, or private, and lasts while the command runs (`Ctrl-C` closes it). Every tunnel is recorded in your organisation's activity log.

Options: `--port N` to choose the local port, `--bind ADDR` to listen on another address, `--show-password` to print the password in the connection string.

## Licence

Apache 2.0
