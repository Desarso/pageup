# Pageup

Pageup turns a local HTML file into an unlisted, shareable URL with one command:

```console
$ pageup report.html
https://pages.gabrielmalek.com/019c...
```

Viewing pages is public so links can be shared. Uploading and key management require an Ed25519-signed request; the server stores public keys only. Every upload gets an immutable UUIDv7 URL, and there is no public page index.

## Install

macOS or Linux:

```sh
curl -fsSL https://pages.gabrielmalek.com/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://pages.gabrielmalek.com/install.ps1 | iex
```

Every Pageup CLI binary contains the complete `$pages` agent skill. Install it into the detected Codex or `~/.agents` skill directory with:

```sh
pageup skill install
```

Use `pageup skill show` to inspect the embedded instructions, `--harness project` to install under `./.agents/skills`, or `--target DIR` for another agent harness. Existing skill files are preserved unless `--force` is supplied.

The initial machine is configured during deployment. For a new computer, create a separate, revocable key:

```sh
pageup init --endpoint https://pages.gabrielmalek.com --name "Gabriel laptop"
```

The command prints a public key and an approval command. Run that approval command on a computer which already has an admin credential, then verify the new computer:

```sh
pageup doctor
pageup example.html
```

This pairing flow never moves a private key between computers. Use `pageup keys list` and `pageup keys revoke KEY_ID` to audit or revoke devices. New keys default to the upload-only role; pass `--role admin` only when the device should also manage keys.

## CLI

```text
pageup file.html                     upload a file; print its URL
pageup -                             upload HTML from stdin
pageup --json file.html              return id, URL, and creation state as JSON
pageup --open file.html              upload and open in the default browser
pageup doctor                        test connectivity and authentication
pageup whoami                        show the active key
pageup public-key                    print this device's public key
pageup skill show                    print the embedded Pages skill
pageup skill install                 install Pages into an agent harness
pageup keys add --name NAME PUBKEY   authorize another device
pageup keys list                     list authorized devices
pageup keys revoke KEY_ID            revoke a device
```

Credentials live at `~/.config/pageup/config.json` on Linux, the normal application config directory on macOS/Windows, and always use mode `0600` where supported. `PAGEUP_CONFIG` selects another config file. Headless agents can use `PAGEUP_PRIVATE_KEY` with `PAGEUP_ENDPOINT` instead; treat the private-key value as a secret.

## Security model

Each request signs the HTTP method, path, Unix timestamp, random UUIDv7 nonce, and SHA-256 body hash. The server rejects unknown keys, modified requests, timestamps outside five minutes, and replayed nonces. Admin-only endpoints add, list, or revoke keys. Upload-only keys cannot manage access.

Pages are capped at 5 MiB, stored outside the container on a persistent volume, served as `text/html`, and immutable. Anyone with a page URL can view it; UUID randomness and the absence of a listing provide link privacy, not access control.

## Development

The project uses only the Go standard library.

```sh
make check
make build
make docker
```

The container builds CLI downloads for Linux, macOS, and Windows on amd64 and arm64. `scripts/render-coolify-dockerfile.sh` produces the self-contained Dockerfile used by the no-Git Coolify deployment.

Server settings:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PAGEUP_PUBLIC_URL` | derived from request | Canonical origin returned after upload |
| `PAGEUP_DATA_DIR` | `/data` | Persistent pages and authorized-key store |
| `PAGEUP_DOWNLOADS_DIR` | `/app/downloads` | Cross-platform CLI binaries |
| `PAGEUP_BOOTSTRAP_KEYS` | required on first boot | JSON array containing at least one admin public key |
| `PAGEUP_MAX_PAGE_BYTES` | `5242880` | Maximum uploaded HTML size |
| `PAGEUP_LISTEN_ADDR` | `:8080` | HTTP listen address |
