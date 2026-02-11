# bb

A browser automation CLI for AI agents. Drives a persistent headless Chrome instance with plain-text output — designed to be called from scripts, pipelines, and LLM tool loops.

Auto-starts Chrome on first use. State stored in `~/.bb/`.

## Install

```bash
brew install devbydaniel/tap/bb
```

Or build from source:

```bash
git clone https://github.com/devbydaniel/bb.git
cd bb
go build -o bb .
```

## Quick start

```bash
bb open https://example.com      # navigate and extract readable content
bb click "a.nav-link"            # click an element
bb text ".main-content"          # extract text from an element
bb screenshot page.png           # take a screenshot
bb stop                          # shut down Chrome
```

## Commands

### Navigate

```
bb open <url>              Navigate and extract readable content
bb open --raw <url>        Navigate without content extraction
bb open --wait <url>       Wait for full DOM stability after load
bb back                    Go back
bb forward                 Go forward
bb reload                  Reload page
```

### Extract

```
bb url                     Print current URL
bb title                   Print page title
bb text [selector]         Print text content (page or element)
bb html [selector]         Print HTML (page or element)
bb attr <selector> <name>  Print attribute value
bb pdf [file]              Save page as PDF
bb extract                 Re-extract readable content from current page
```

### Interact

```
bb click <selector>        Click element
bb input <selector> <text> Type into input field
bb clear <selector>        Clear input field
bb select <selector> <val> Select dropdown option
bb submit <selector>       Submit form
bb hover <selector>        Hover over element
bb focus <selector>        Focus element
```

### JavaScript

```
bb js <expression>         Evaluate JS expression
```

### Wait

```
bb wait <selector>         Wait for element to appear
bb waitload                Wait for page load event
bb waitstable              Wait for DOM to stop changing
bb waitidle                Wait for network idle
bb sleep <seconds>         Sleep N seconds
```

### Screenshots

```
bb screenshot [file] [-w N] [-h N]   Page screenshot
bb screenshot-el <sel> [file]        Element screenshot
```

### Tabs

```
bb pages                   List all tabs
bb page <index>            Switch to tab
bb newpage [url]           Open new tab
bb closepage [index]       Close tab
```

### Query

```
bb exists <selector>       Check if element exists (exit code)
bb count <selector>        Count matching elements
bb visible <selector>      Check if element is visible (exit code)
```

### Accessibility

```
bb ax-tree [--depth N]             Dump accessibility tree
bb ax-find [--name N] [--role R]   Find accessible nodes
bb ax-node <selector>              Inspect element accessibility
```

### Browser

```
bb status                  Show browser status
bb stop                    Shut down Chrome
```

## Flags

| Flag | Description |
|------|-------------|
| `--json` | JSON output (supported by: open, extract, js, pages, status, ax-tree, ax-find, ax-node) |
| `--timeout <seconds>` | Override default timeout (default: 30) |

## Environment variables

| Variable | Description |
|----------|-------------|
| `BB_CHROME_BIN` | Path to Chrome/Chromium binary |
| `BB_TIMEOUT` | Default timeout in seconds |

## Tips

- For dynamic pages, prefer `bb wait <selector>` or `bb sleep <N>` after `bb open`
- Most modern sites are SPAs — start with `bb open`, not `bb open --wait`
- All commands output plain text by default; use `--json` for structured output

## License

MIT
