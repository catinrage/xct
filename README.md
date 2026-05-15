# XCT

XCT is an Xray CDN Tunnel controller.

It is a Go TUI application for creating and managing Xray tunnels between an Iran VPS behind a CDN and an outer VPS. The TUI is built with Bubble Tea and replaces the older shell controller workflow with a profile-based interface.

## What It Does

XCT creates two tunnel types:

### Reverse: Iran -> outer exit

Traffic enters a local SOCKS or VLESS inbound on the Iran VPS, crosses the CDN through an Xray reverse tunnel, and exits from the outer VPS.

Use this when the outer VPS can reach the Iran CDN domain, but the Iran VPS should not connect directly to the outer VPS.

### Direct: outer -> Iran exit

Traffic enters a local SOCKS or VLESS inbound on the outer VPS, crosses the CDN using VLESS over WebSocket/TLS, reaches nginx on the Iran VPS, and exits from Iran.

Use this when applications on the outer VPS need an Iran exit.

## Features

- Bubble Tea TUI
- profile-based create/manage flow
- reverse and direct tunnel creation
- x-ui outbound snippet generation
- built-in test/debug/status actions
- rescue action for half-created profiles
- automatic outer VPS bootstrap
- installs nginx on the outer VPS if missing
- installs Xray on the outer VPS using `XTLS/Xray-install` if missing
- optional SSH through SOCKS5
- optional TCP/BBR tuning on both servers
- commit-based GitHub release builds
- self-update from the TUI
- optional SOCKS5 proxy for GitHub update checks and downloads

## Requirements

Run `xct-controller` on the Iran VPS as root.

Iran VPS:

- root shell
- nginx installed
- Xray installed locally, commonly at `/usr/local/x-ui/bin/xray-linux-amd64`
- valid TLS certificate and key for the CDN domain
- SSH client
- `curl`
- optional `sshpass` for password SSH
- optional `nc` or `ncat` for SSH through SOCKS5

Outer VPS:

- fresh Linux VPS is supported
- SSH access as root, or a user with passwordless sudo
- internet access for package installs and Xray installer

The controller handles nginx and Xray installation on the outer VPS.

## Build

```bash
GOPROXY=direct GOSUMDB=sum.golang.org /usr/local/go/bin/go test ./...
GOPROXY=direct GOSUMDB=sum.golang.org /usr/local/go/bin/go build -o xct-controller ./cmd/xct-controller
```

## Run

```bash
sudo ./xct-controller
```

The app refuses to start without root because it writes nginx, systemd, and Xray config files on the Iran VPS.

## TUI Flow

The first screen shows:

- create reverse profile
- create direct profile
- update
- settings
- existing profiles

Select a profile to open its action menu:

- show
- outbound snippet
- test
- debug
- status
- start / stop / restart
- enable / disable
- rescue
- tune
- delete

During profile creation:

- `enter` advances to the next field
- `up` / `down` moves between fields
- `space` toggles checkboxes and option fields
- `left` / `right` changes option fields
- `ctrl+i` opens field help
- `esc` returns to the menu

## Updates

The `Update` main-menu item checks GitHub releases and installs the newest `linux_amd64` release archive over the running `xct-controller` binary.

By default, update checks use:

```text
catinrage/xct
```

The release workflow publishes:

```text
xct_VERSION_linux_amd64.tar.gz
xct_VERSION_linux_amd64.tar.gz.sha256
```

Pushes to `main` create prerelease builds named like `0.0.RUN-SHA`. Tags matching `v*` create normal releases.

If GitHub is not directly reachable from the Iran VPS, open `Settings` from the main menu and configure the SOCKS5 proxy used for update checks and downloads.

The settings are stored at:

```text
/etc/xray-cdn-tunnel-controller/settings.env
```

## Rescue

Profile state is saved before deployment starts. If creation fails midway, select the profile and run `Rescue`.

Rescue reruns the idempotent deployment steps:

- regenerate Iran config files
- reload nginx/systemd
- bootstrap the outer VPS
- upload outer Xray config
- upload outer systemd service
- start/enable services

## Generated Files

Controller state:

```text
/etc/xray-cdn-tunnel-controller/
/etc/xray-cdn-tunnel-controller/profiles/PROFILE/profile.env
```

Iran generated files:

```text
/etc/xray-cdn-tunnel-controller/PROFILE-iran-*.json
/etc/systemd/system/xct-*-PROFILE-iran.service
/etc/nginx/sites-available/xct-PROFILE-*.conf
/etc/nginx/sites-enabled/xct-PROFILE-*.conf
```

Outer generated files:

```text
/etc/xray-cdn-tunnel-controller/PROFILE-outer-*.json
/etc/systemd/system/xct-*-PROFILE-outer.service
```

## Notes

Use unique CDN ports and WebSocket paths per profile. Avoid ports already used by x-ui, old nginx configs, FRP, GOST, or other tunnel profiles.

For x-ui, prefer the generated local VLESS outbound snippet when possible. SOCKS snippets are also shown for testing and compatibility.
