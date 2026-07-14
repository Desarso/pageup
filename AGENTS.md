# Sharing an HTML page

When a human needs to inspect or share a generated HTML artifact, upload it with:

```sh
pageup path/to/page.html
```

The command prints only the shareable URL on success. Use `pageup --json page.html` when a machine-readable response is preferable, or pipe content with `pageup -`.

Run `pageup doctor` if credentials or connectivity are in doubt. Never print, commit, or copy the private key from `~/.config/pageup/config.json`.
