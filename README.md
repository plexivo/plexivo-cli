# Plexivo CLI

Connect to your Plexivo databases and caches from your own machine — public or private — through a short-lived, encrypted tunnel.

## Install

macOS and Linux:

```sh
curl -fsSL https://github.com/plexivo/plexivo-cli/releases/latest/download/install.sh | sh
```

Windows: download `plexivo-windows-amd64.zip` from the [latest release](https://github.com/plexivo/plexivo-cli/releases/latest) and put `plexivo.exe` on your `PATH`.

## Sign in

Sign in through your browser, the way `gcloud auth login` works:

```sh
plexivo auth login
```

The CLI shows a short code and opens the console. Check that the code matches, choose the organisation and what the session may do, and approve:

| Access | What the session may do |
| --- | --- |
| Read only | See resources, logs and settings. Changes nothing. |
| Operate | Create and change resources and open database tunnels. No deletes, no billing. |
| Full | Everything you can do in that organisation, including deletes. |

A session never has more access than your own role, or than the organisation allows. It ends on its own after the organisation's session length (1, 4, 8, 12 or 24 hours, 8 by default; set it in **Settings → CLI sessions**), and the CLI then asks you to sign in again.

```sh
plexivo auth status     # organisation, access and the time left
plexivo auth logout     # end the session now
```

You can also see and end every CLI session in **Settings → CLI sessions**.

On a server without a browser, run `plexivo auth login --no-browser` and open the link on your phone or laptop.

For CI and automation, create an API key in the console (**Settings → API keys**) and set `PLEXIVO_TOKEN`, or store one with `plexivo auth login --api-key`. The stored sign-in lives in your user configuration directory, readable only by you.

## Connect to a database

Quickstart: create a Plexivo Postgres database in the console and wait until it is running. Sign in above with **Operate** access, then list the resources and connect using the exact resource name:

```sh
plexivo db list
plexivo db connect orders-db
```

`plexivo db connect` opens a local port — 5432 for Postgres, 6380 for Cache, or a free one — and prints the connection details. Keep it running and use `psql`, TablePlus or DBeaver in a second terminal. Supply the database password securely from the console when your client prompts for it, rather than putting it in shell history. The tunnel works whether the database is public, limited to trusted networks, or private, and lasts while the command runs (`Ctrl-C` closes it). Every tunnel is recorded in your organisation's activity log.

Confirm the connection with `SELECT current_database(), current_user;`. When finished, close the tunnel with `Ctrl-C` and run `plexivo auth logout` on a shared machine. This administrative tunnel requires the Plexivo control plane; use the resource's verified-TLS direct endpoint with a persistent connection pool for application traffic.

Options: `--port N` to choose the local port, `--bind ADDR` to listen on another address, `--show-password` to print the password in the connection string.

## Licence

Apache 2.0
