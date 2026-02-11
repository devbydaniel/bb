package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var (
	bbBin   string
	server  *httptest.Server
	tempHome string
)

// HTML pages served by the test server

const indexHTML = `<!DOCTYPE html>
<html><head><title>Test Page</title></head>
<body>
<h1>Hello World</h1>
<p id="intro">This is a test page for bb.</p>
<p class="content">Paragraph two.</p>
<a id="link1" href="/page2" data-foo="bar">Go to page 2</a>
<div id="hidden" style="display:none">Hidden content</div>
<img id="logo" alt="Test Logo" src="/logo.png">
</body></html>`

const page2HTML = `<!DOCTYPE html>
<html><head><title>Page Two</title></head>
<body>
<h1>Page Two</h1>
<p>You navigated here.</p>
<a href="/">Back to home</a>
</body></html>`

const formHTML = `<!DOCTYPE html>
<html><head><title>Form Page</title></head>
<body>
<h1>Form</h1>
<form id="myform" action="/submitted" method="get">
  <input id="name" type="text" name="name" value="" placeholder="Name">
  <textarea id="bio" name="bio"></textarea>
  <select id="color" name="color">
    <option value="red">Red</option>
    <option value="blue">Blue</option>
    <option value="green">Green</option>
  </select>
  <button id="submitbtn" type="submit">Submit</button>
</form>
</body></html>`

const delayedHTML = `<!DOCTYPE html>
<html><head><title>Delayed Page</title></head>
<body>
<h1>Delayed</h1>
<script>
setTimeout(function() {
  var el = document.createElement('div');
  el.id = 'delayed-el';
  el.textContent = 'I appeared!';
  document.body.appendChild(el);
}, 500);
</script>
</body></html>`

var bigHTML = `<!DOCTYPE html>
<html><head><title>Big Page</title></head>
<body>
<h1>Big Page</h1>
<p>` + strings.Repeat("Lorem ipsum dolor sit amet. ", 3000) + `</p>
</body></html>`

const submittedHTML = `<!DOCTYPE html>
<html><head><title>Submitted</title></head>
<body><h1>Form Submitted</h1></body></html>`

const multiElHTML = `<!DOCTYPE html>
<html><head><title>Multi</title></head>
<body>
<ul>
  <li class="item">One</li>
  <li class="item">Two</li>
  <li class="item">Three</li>
</ul>
<button id="btn" aria-label="Click Me" role="button">Click</button>
</body></html>`

func TestMain(m *testing.M) {
	// Set up test HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, indexHTML)
	})
	mux.HandleFunc("/page2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, page2HTML)
	})
	mux.HandleFunc("/form", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, formHTML)
	})
	mux.HandleFunc("/delayed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, delayedHTML)
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, bigHTML)
	})
	mux.HandleFunc("/submitted", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, submittedHTML)
	})
	mux.HandleFunc("/multi", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, multiElHTML)
	})
	server = httptest.NewServer(mux)

	// Build binary
	tmp, err := os.MkdirTemp("", "bb-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	tempHome = tmp
	bbBin = filepath.Join(tmp, "bb")

	build := exec.Command("go", "build", "-o", bbBin, ".")
	build.Dir = "."
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build bb: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	// Teardown: stop browser, clean up
	runBBRaw("stop")
	server.Close()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// runBBRaw executes bb with args and returns stdout, stderr, exit code
func runBBRaw(args ...string) (string, string, int) {
	cmd := exec.Command(bbBin, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+tempHome,
		"BB_TIMEOUT=15",
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

// runBB executes bb, fails test on non-zero exit
func runBB(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := runBBRaw(args...)
	if code != 0 {
		t.Fatalf("bb %v failed (exit %d)\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
	}
	return stdout
}

// --- Tests ---

func TestOpenAndExtract(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		out := runBB(t, "open", server.URL+"/")
		if !strings.Contains(out, "Hello World") {
			t.Errorf("expected 'Hello World' in output, got: %s", out)
		}
		if !strings.Contains(out, "test page for bb") {
			t.Errorf("expected 'test page for bb' in output, got: %s", out)
		}
	})

	t.Run("open --raw", func(t *testing.T) {
		out := runBB(t, "open", "--raw", server.URL+"/")
		if !strings.Contains(out, "Test Page") {
			t.Errorf("expected title 'Test Page' in raw output, got: %s", out)
		}
	})

	t.Run("open --raw --json", func(t *testing.T) {
		out := runBB(t, "open", "--raw", "--json", server.URL+"/")
		var result map[string]string
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if result["title"] != "Test Page" {
			t.Errorf("expected title 'Test Page', got %q", result["title"])
		}
	})

	t.Run("open --json", func(t *testing.T) {
		out := runBB(t, "open", "--json", server.URL+"/")
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if _, ok := result["content"]; !ok {
			t.Error("expected 'content' key in JSON output")
		}
		if result["truncated"] != false {
			t.Error("expected truncated=false")
		}
	})

	t.Run("open --wait", func(t *testing.T) {
		out := runBB(t, "open", "--wait", server.URL+"/")
		if !strings.Contains(out, "Hello World") {
			t.Errorf("expected content after --wait, got: %s", out)
		}
	})

	t.Run("extract", func(t *testing.T) {
		runBB(t, "open", "--raw", server.URL+"/")
		out := runBB(t, "extract")
		if !strings.Contains(out, "Hello World") || !strings.Contains(out, "test page for bb") {
			t.Errorf("extract didn't return page content: %s", out)
		}
	})

	t.Run("extract --json", func(t *testing.T) {
		out := runBB(t, "extract", "--json")
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if _, ok := result["content"]; !ok {
			t.Error("expected 'content' key")
		}
	})

	t.Run("open auto-prepends https", func(t *testing.T) {
		// This tests the URL normalization — we can't actually test https here
		// but we can verify open doesn't crash with a full URL
		out := runBB(t, "open", "--raw", server.URL+"/")
		if !strings.Contains(out, "Test Page") {
			t.Error("open with full URL failed")
		}
	})
}

func TestURLAndTitle(t *testing.T) {
	runBB(t, "open", "--raw", server.URL+"/")

	t.Run("url", func(t *testing.T) {
		out := runBB(t, "url")
		if !strings.Contains(out, server.URL) {
			t.Errorf("expected URL containing %s, got: %s", server.URL, out)
		}
	})

	t.Run("title", func(t *testing.T) {
		out := runBB(t, "title")
		if !strings.Contains(out, "Test Page") {
			t.Errorf("expected 'Test Page', got: %s", out)
		}
	})
}

func TestTextAndHTML(t *testing.T) {
	runBB(t, "open", "--raw", server.URL+"/")

	t.Run("text full page", func(t *testing.T) {
		out := runBB(t, "text")
		if !strings.Contains(out, "Hello World") {
			t.Errorf("expected 'Hello World' in text, got: %s", out)
		}
	})

	t.Run("text with selector", func(t *testing.T) {
		out := runBB(t, "text", "#intro")
		expected := "This is a test page for bb."
		if !strings.Contains(out, expected) {
			t.Errorf("expected %q, got: %s", expected, out)
		}
	})

	t.Run("html full page", func(t *testing.T) {
		out := runBB(t, "html")
		if !strings.Contains(out, "<h1>Hello World</h1>") {
			t.Errorf("expected h1 in HTML output, got: %s", out[:min(200, len(out))])
		}
	})

	t.Run("html with selector", func(t *testing.T) {
		out := runBB(t, "html", "#intro")
		if !strings.Contains(out, "test page for bb") {
			t.Errorf("expected intro text in HTML, got: %s", out)
		}
	})

	t.Run("text invalid selector", func(t *testing.T) {
		_, _, code := runBBRaw("text", "#nonexistent")
		if code == 0 {
			t.Error("expected non-zero exit for invalid selector")
		}
	})
}

func TestAttr(t *testing.T) {
	runBB(t, "open", "--raw", server.URL+"/")

	t.Run("existing attribute", func(t *testing.T) {
		out := runBB(t, "attr", "#link1", "data-foo")
		if strings.TrimSpace(out) != "bar" {
			t.Errorf("expected 'bar', got: %q", strings.TrimSpace(out))
		}
	})

	t.Run("href attribute", func(t *testing.T) {
		out := runBB(t, "attr", "#link1", "href")
		if strings.TrimSpace(out) != "/page2" {
			t.Errorf("expected '/page2', got: %q", strings.TrimSpace(out))
		}
	})

	t.Run("missing attribute", func(t *testing.T) {
		_, _, code := runBBRaw("attr", "#link1", "data-nope")
		if code == 0 {
			t.Error("expected non-zero exit for missing attribute")
		}
	})

	t.Run("missing args", func(t *testing.T) {
		_, _, code := runBBRaw("attr", "#link1")
		if code == 0 {
			t.Error("expected non-zero exit for missing args")
		}
	})
}

func TestNavigation(t *testing.T) {
	// Open page 1, then page 2, test back/forward
	runBB(t, "open", "--raw", server.URL+"/")
	runBB(t, "open", "--raw", server.URL+"/page2")

	t.Run("back", func(t *testing.T) {
		out := runBB(t, "back")
		if !strings.Contains(out, server.URL) {
			t.Errorf("back didn't return URL: %s", out)
		}
		title := runBB(t, "title")
		if !strings.Contains(title, "Test Page") {
			t.Errorf("after back, expected 'Test Page', got: %s", title)
		}
	})

	t.Run("forward", func(t *testing.T) {
		runBB(t, "forward")
		title := runBB(t, "title")
		if !strings.Contains(title, "Page Two") {
			t.Errorf("after forward, expected 'Page Two', got: %s", title)
		}
	})

	t.Run("reload", func(t *testing.T) {
		out := runBB(t, "reload")
		if !strings.Contains(out, "Reloaded") {
			t.Errorf("expected 'Reloaded', got: %s", out)
		}
	})
}

func TestInteraction(t *testing.T) {
	runBB(t, "open", "--raw", server.URL+"/form")

	t.Run("input", func(t *testing.T) {
		out := runBB(t, "input", "#name", "Alice")
		if !strings.Contains(out, "Typed") {
			t.Errorf("expected 'Typed', got: %s", out)
		}
		// Verify the value was set
		val := runBB(t, "js", `document.querySelector('#name').value`)
		if !strings.Contains(val, "Alice") {
			t.Errorf("expected input value 'Alice', got: %s", val)
		}
	})

	t.Run("clear", func(t *testing.T) {
		runBB(t, "input", "#name", "to-be-cleared")
		out := runBB(t, "clear", "#name")
		if !strings.Contains(out, "Cleared") {
			t.Errorf("expected 'Cleared', got: %s", out)
		}
		val := runBB(t, "js", `document.querySelector('#name').value`)
		val = strings.TrimSpace(val)
		if val != "" {
			t.Errorf("expected empty value after clear, got: %q", val)
		}
	})

	t.Run("select", func(t *testing.T) {
		out := runBB(t, "select", "#color", "blue")
		if !strings.Contains(out, "Selected") {
			t.Errorf("expected 'Selected', got: %s", out)
		}
		val := runBB(t, "js", `document.querySelector('#color').value`)
		if !strings.Contains(val, "blue") {
			t.Errorf("expected 'blue', got: %s", val)
		}
	})

	t.Run("hover", func(t *testing.T) {
		out := runBB(t, "hover", "#submitbtn")
		if !strings.Contains(out, "Hovered") {
			t.Errorf("expected 'Hovered', got: %s", out)
		}
	})

	t.Run("focus", func(t *testing.T) {
		out := runBB(t, "focus", "#name")
		if !strings.Contains(out, "Focused") {
			t.Errorf("expected 'Focused', got: %s", out)
		}
	})

	t.Run("click", func(t *testing.T) {
		out := runBB(t, "click", "#submitbtn")
		if !strings.Contains(out, "Clicked") {
			t.Errorf("expected 'Clicked', got: %s", out)
		}
	})

	t.Run("submit", func(t *testing.T) {
		runBB(t, "open", "--raw", server.URL+"/form")
		out := runBB(t, "submit", "#myform")
		if !strings.Contains(out, "Submitted") {
			t.Errorf("expected 'Submitted', got: %s", out)
		}
	})

	t.Run("click invalid selector", func(t *testing.T) {
		_, _, code := runBBRaw("click", "#nonexistent")
		if code == 0 {
			t.Error("expected error for invalid selector")
		}
	})

	t.Run("input missing args", func(t *testing.T) {
		_, _, code := runBBRaw("input", "#name")
		if code == 0 {
			t.Error("expected error for missing text arg")
		}
	})
}

func TestJS(t *testing.T) {
	runBB(t, "open", "--raw", server.URL+"/")

	t.Run("string expression", func(t *testing.T) {
		out := runBB(t, "js", `document.title`)
		if !strings.Contains(out, "Test Page") {
			t.Errorf("expected 'Test Page', got: %s", out)
		}
	})

	t.Run("number expression", func(t *testing.T) {
		out := runBB(t, "js", `1 + 2`)
		if strings.TrimSpace(out) != "3" {
			t.Errorf("expected '3', got: %q", strings.TrimSpace(out))
		}
	})

	t.Run("boolean expression", func(t *testing.T) {
		out := runBB(t, "js", `true`)
		if strings.TrimSpace(out) != "true" {
			t.Errorf("expected 'true', got: %q", strings.TrimSpace(out))
		}
	})

	t.Run("null expression", func(t *testing.T) {
		out := runBB(t, "js", `null`)
		if strings.TrimSpace(out) != "null" {
			t.Errorf("expected 'null', got: %q", strings.TrimSpace(out))
		}
	})

	t.Run("object expression", func(t *testing.T) {
		out := runBB(t, "js", `({a: 1, b: "two"})`)
		if !strings.Contains(out, `"a"`) || !strings.Contains(out, `"two"`) {
			t.Errorf("expected JSON object, got: %s", out)
		}
	})

	t.Run("array expression", func(t *testing.T) {
		out := runBB(t, "js", `[1, 2, 3]`)
		if !strings.Contains(out, "1") || !strings.Contains(out, "3") {
			t.Errorf("expected array, got: %s", out)
		}
	})

	t.Run("json flag", func(t *testing.T) {
		out := runBB(t, "js", "--json", `42`)
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("invalid JSON: %v\nraw: %s", err, out)
		}
		if result["value"] != float64(42) {
			t.Errorf("expected value=42, got: %v", result["value"])
		}
	})

	t.Run("missing expression", func(t *testing.T) {
		_, _, code := runBBRaw("js")
		if code == 0 {
			t.Error("expected error for missing JS expression")
		}
	})
}

func TestWaiting(t *testing.T) {
	t.Run("wait for delayed element", func(t *testing.T) {
		runBB(t, "open", "--raw", server.URL+"/delayed")
		out := runBB(t, "wait", "#delayed-el")
		if !strings.Contains(out, "visible") {
			t.Errorf("expected 'Element visible', got: %s", out)
		}
		text := runBB(t, "text", "#delayed-el")
		if !strings.Contains(text, "I appeared!") {
			t.Errorf("expected delayed element text, got: %s", text)
		}
	})

	t.Run("waitload", func(t *testing.T) {
		runBB(t, "open", "--raw", server.URL+"/")
		out := runBB(t, "waitload")
		if !strings.Contains(out, "loaded") {
			t.Errorf("expected 'Page loaded', got: %s", out)
		}
	})

	t.Run("waitstable", func(t *testing.T) {
		out := runBB(t, "waitstable")
		if !strings.Contains(out, "stable") {
			t.Errorf("expected 'DOM stable', got: %s", out)
		}
	})

	t.Run("waitidle", func(t *testing.T) {
		out := runBB(t, "waitidle")
		if !strings.Contains(out, "idle") {
			t.Errorf("expected 'Network idle', got: %s", out)
		}
	})

	t.Run("sleep", func(t *testing.T) {
		_, _, code := runBBRaw("sleep", "0.1")
		if code != 0 {
			t.Error("sleep 0.1 should succeed")
		}
	})

	t.Run("sleep invalid", func(t *testing.T) {
		_, _, code := runBBRaw("sleep", "abc")
		if code == 0 {
			t.Error("expected error for invalid sleep duration")
		}
	})

	t.Run("wait missing selector", func(t *testing.T) {
		_, _, code := runBBRaw("wait")
		if code == 0 {
			t.Error("expected error for missing selector")
		}
	})
}

func TestScreenshots(t *testing.T) {
	runBB(t, "open", "--raw", server.URL+"/")
	dir := t.TempDir()

	t.Run("screenshot default", func(t *testing.T) {
		// Run from temp dir so auto-named file lands there
		file := filepath.Join(dir, "shot.png")
		out := runBB(t, "screenshot", file)
		if !strings.Contains(out, file) {
			t.Errorf("expected filename in output, got: %s", out)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatalf("screenshot file not found: %v", err)
		}
		if info.Size() == 0 {
			t.Error("screenshot file is empty")
		}
	})

	t.Run("screenshot with dimensions", func(t *testing.T) {
		file := filepath.Join(dir, "sized.png")
		out := runBB(t, "screenshot", "-w", "800", "-h", "600", file)
		if !strings.Contains(out, file) {
			t.Errorf("expected filename in output, got: %s", out)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatalf("screenshot file not found: %v", err)
		}
		if info.Size() == 0 {
			t.Error("screenshot file is empty")
		}
	})

	t.Run("screenshot-el", func(t *testing.T) {
		file := filepath.Join(dir, "element.png")
		out := runBB(t, "screenshot-el", "h1", file)
		if !strings.Contains(out, "Saved") {
			t.Errorf("expected 'Saved', got: %s", out)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatalf("element screenshot not found: %v", err)
		}
		if info.Size() == 0 {
			t.Error("element screenshot is empty")
		}
	})

	t.Run("screenshot-el invalid", func(t *testing.T) {
		_, _, code := runBBRaw("screenshot-el", "#nonexistent", filepath.Join(dir, "nope.png"))
		if code == 0 {
			t.Error("expected error for invalid selector")
		}
	})
}

func TestPDF(t *testing.T) {
	runBB(t, "open", "--raw", server.URL+"/")
	dir := t.TempDir()

	t.Run("pdf", func(t *testing.T) {
		file := filepath.Join(dir, "test.pdf")
		out := runBB(t, "pdf", file)
		if !strings.Contains(out, "Saved") {
			t.Errorf("expected 'Saved', got: %s", out)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatalf("PDF not found: %v", err)
		}
		if info.Size() == 0 {
			t.Error("PDF is empty")
		}
	})
}

func TestTabs(t *testing.T) {
	// Start fresh — stop and reopen
	runBB(t, "open", "--raw", server.URL+"/")

	t.Run("pages", func(t *testing.T) {
		out := runBB(t, "pages")
		if !strings.Contains(out, "[0]") {
			t.Errorf("expected page listing, got: %s", out)
		}
		if !strings.Contains(out, "*") {
			t.Errorf("expected active marker, got: %s", out)
		}
	})

	t.Run("pages --json", func(t *testing.T) {
		out := runBB(t, "pages", "--json")
		var pages []map[string]interface{}
		if err := json.Unmarshal([]byte(out), &pages); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(pages) == 0 {
			t.Error("expected at least one page")
		}
	})

	t.Run("newpage", func(t *testing.T) {
		out := runBB(t, "newpage", server.URL+"/page2")
		if !strings.Contains(out, "Opened") {
			t.Errorf("expected 'Opened', got: %s", out)
		}
		// Verify we now have 2+ pages
		pagesOut := runBB(t, "pages")
		if !strings.Contains(pagesOut, "[1]") {
			t.Errorf("expected second page, got: %s", pagesOut)
		}
	})

	t.Run("page switch", func(t *testing.T) {
		out := runBB(t, "page", "0")
		if !strings.Contains(out, "Switched") {
			t.Errorf("expected 'Switched', got: %s", out)
		}
	})

	t.Run("page out of range", func(t *testing.T) {
		_, _, code := runBBRaw("page", "99")
		if code == 0 {
			t.Error("expected error for out of range index")
		}
	})

	t.Run("closepage", func(t *testing.T) {
		// Open a third page so we can close one safely
		runBB(t, "newpage", server.URL+"/form")
		out := runBB(t, "closepage", "2")
		if !strings.Contains(out, "Closed") {
			t.Errorf("expected 'Closed', got: %s", out)
		}
	})

	t.Run("closepage last page", func(t *testing.T) {
		// Close until one remains, then try to close it
		pages := runBB(t, "pages", "--json")
		var ps []map[string]interface{}
		_ = json.Unmarshal([]byte(pages), &ps)
		for len(ps) > 1 {
			runBB(t, "closepage", "0")
			pages = runBB(t, "pages", "--json")
			_ = json.Unmarshal([]byte(pages), &ps)
		}
		_, _, code := runBBRaw("closepage")
		if code == 0 {
			t.Error("expected error when closing last page")
		}
	})

	t.Run("newpage blank", func(t *testing.T) {
		out := runBB(t, "newpage")
		if !strings.Contains(out, "Opened") {
			t.Errorf("expected 'Opened', got: %s", out)
		}
		// Clean up
		runBB(t, "closepage")
	})
}

func TestQuery(t *testing.T) {
	runBB(t, "open", "--raw", server.URL+"/multi")

	t.Run("exists true", func(t *testing.T) {
		out, _, code := runBBRaw("exists", ".item")
		if code != 0 {
			t.Error("expected exit 0 for existing element")
		}
		if !strings.Contains(out, "true") {
			t.Errorf("expected 'true', got: %s", out)
		}
	})

	t.Run("exists false", func(t *testing.T) {
		out, _, code := runBBRaw("exists", "#nonexistent")
		if code == 0 {
			t.Error("expected exit 1 for non-existing element")
		}
		if !strings.Contains(out, "false") {
			t.Errorf("expected 'false', got: %s", out)
		}
	})

	t.Run("count", func(t *testing.T) {
		out := runBB(t, "count", ".item")
		if strings.TrimSpace(out) != "3" {
			t.Errorf("expected '3', got: %q", strings.TrimSpace(out))
		}
	})

	t.Run("count zero", func(t *testing.T) {
		out := runBB(t, "count", ".nonexistent")
		if strings.TrimSpace(out) != "0" {
			t.Errorf("expected '0', got: %q", strings.TrimSpace(out))
		}
	})

	t.Run("visible true", func(t *testing.T) {
		out, _, code := runBBRaw("visible", "#btn")
		if code != 0 {
			t.Error("expected exit 0 for visible element")
		}
		if !strings.Contains(out, "true") {
			t.Errorf("expected 'true', got: %s", out)
		}
	})

	t.Run("visible false hidden element", func(t *testing.T) {
		runBB(t, "open", "--raw", server.URL+"/")
		out, _, code := runBBRaw("visible", "#hidden")
		if code == 0 {
			t.Error("expected exit 1 for hidden element")
		}
		if !strings.Contains(out, "false") {
			t.Errorf("expected 'false', got: %s", out)
		}
	})

	t.Run("visible nonexistent", func(t *testing.T) {
		_, _, code := runBBRaw("visible", "#nonexistent")
		if code == 0 {
			t.Error("expected exit 1 for nonexistent element")
		}
	})
}

func TestAccessibility(t *testing.T) {
	runBB(t, "open", "--raw", "--wait", server.URL+"/multi")

	t.Run("ax-tree", func(t *testing.T) {
		out := runBB(t, "ax-tree")
		if out == "" {
			t.Error("expected non-empty ax-tree output")
		}
		// Should contain some role markers
		if !strings.Contains(out, "[") {
			t.Errorf("expected role markers in ax-tree, got: %s", out[:min(200, len(out))])
		}
	})

	t.Run("ax-tree --depth", func(t *testing.T) {
		out := runBB(t, "ax-tree", "--depth", "2")
		if out == "" {
			t.Error("expected non-empty ax-tree output with depth limit")
		}
	})

	t.Run("ax-tree --json", func(t *testing.T) {
		out := runBB(t, "ax-tree", "--json")
		var nodes []interface{}
		if err := json.Unmarshal([]byte(out), &nodes); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(nodes) == 0 {
			t.Error("expected non-empty ax-tree nodes")
		}
	})

	t.Run("ax-find --role", func(t *testing.T) {
		out := runBB(t, "ax-find", "--timeout", "30", "--role", "button")
		if !strings.Contains(out, "button") {
			t.Errorf("expected button role in output, got: %s", out)
		}
	})

	t.Run("ax-find --name", func(t *testing.T) {
		out := runBB(t, "ax-find", "--timeout", "30", "--name", "Click Me")
		if !strings.Contains(out, "Click Me") {
			t.Errorf("expected 'Click Me' in output, got: %s", out)
		}
	})

	t.Run("ax-find no match", func(t *testing.T) {
		_, _, code := runBBRaw("ax-find", "--name", "NonexistentLabel12345")
		if code == 0 {
			t.Error("expected exit 1 for no matching nodes")
		}
	})

	t.Run("ax-find --json", func(t *testing.T) {
		out := runBB(t, "ax-find", "--json", "--timeout", "30", "--role", "button")
		var nodes []interface{}
		if err := json.Unmarshal([]byte(out), &nodes); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
	})

	t.Run("ax-node", func(t *testing.T) {
		out := runBB(t, "ax-node", "#btn")
		if !strings.Contains(out, "role:") {
			t.Errorf("expected 'role:' in ax-node output, got: %s", out)
		}
	})

	t.Run("ax-node --json", func(t *testing.T) {
		out := runBB(t, "ax-node", "--json", "#btn")
		var node map[string]interface{}
		if err := json.Unmarshal([]byte(out), &node); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
	})

	t.Run("ax-node invalid", func(t *testing.T) {
		_, _, code := runBBRaw("ax-node", "#nonexistent")
		if code == 0 {
			t.Error("expected error for invalid selector")
		}
	})
}

func TestBigPageTruncation(t *testing.T) {
	out := runBB(t, "open", "--json", server.URL+"/big")
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	content, ok := result["content"].(string)
	if !ok {
		t.Fatal("expected content string")
	}
	if len(content) > 52*1024 { // allow slight overhead
		t.Errorf("content should be truncated to ~50KB, got %d bytes", len(content))
	}
	if result["truncated"] != true {
		t.Error("expected truncated=true for big page")
	}
}

func TestStatusAndStop(t *testing.T) {
	// Make sure browser is running
	runBB(t, "open", "--raw", server.URL+"/")

	t.Run("status", func(t *testing.T) {
		out := runBB(t, "status")
		if !strings.Contains(out, "running") && !strings.Contains(out, "PID") {
			t.Errorf("expected running status, got: %s", out)
		}
	})

	t.Run("status --json", func(t *testing.T) {
		out := runBB(t, "status", "--json")
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if result["running"] != true {
			t.Error("expected running=true")
		}
		if _, ok := result["pid"]; !ok {
			t.Error("expected pid in status")
		}
	})

	t.Run("stop and restart", func(t *testing.T) {
		out := runBB(t, "stop")
		if !strings.Contains(out, "stopped") {
			t.Errorf("expected 'stopped', got: %s", out)
		}

		// Status should show not running
		statusOut, _, _ := runBBRaw("status")
		if !strings.Contains(statusOut, "No active") && !strings.Contains(statusOut, "not") {
			// Check JSON
			jsonOut, _, _ := runBBRaw("status", "--json")
			var s map[string]interface{}
			_ = json.Unmarshal([]byte(jsonOut), &s)
			if s["running"] == true {
				t.Error("browser should not be running after stop")
			}
		}

		// Auto-restart on next command
		runBB(t, "open", "--raw", server.URL+"/")
		title := runBB(t, "title")
		if !strings.Contains(title, "Test Page") {
			t.Errorf("expected auto-restart to work, got: %s", title)
		}
	})
}

func TestHelp(t *testing.T) {
	t.Run("help command", func(t *testing.T) {
		out := runBB(t, "help")
		if !strings.Contains(out, "bb") && !strings.Contains(out, "NAVIGATE") {
			t.Errorf("expected help text, got: %s", out[:min(100, len(out))])
		}
	})

	t.Run("--help flag", func(t *testing.T) {
		out := runBB(t, "--help")
		if !strings.Contains(out, "NAVIGATE") {
			t.Errorf("expected help text, got: %s", out[:min(100, len(out))])
		}
	})

	t.Run("no args shows help", func(t *testing.T) {
		out, _, code := runBBRaw()
		if code == 0 {
			t.Error("expected non-zero exit with no args")
		}
		if !strings.Contains(out, "NAVIGATE") {
			t.Errorf("expected help text on no args, got: %s", out[:min(100, len(out))])
		}
	})

	t.Run("unknown command", func(t *testing.T) {
		_, stderr, code := runBBRaw("foobar")
		if code == 0 {
			t.Error("expected non-zero exit for unknown command")
		}
		if !strings.Contains(stderr, "unknown") {
			t.Errorf("expected 'unknown command' in stderr, got: %s", stderr)
		}
	})
}

func TestTimeout(t *testing.T) {
	t.Run("--timeout flag", func(t *testing.T) {
		// Just verify the flag is accepted without error
		out := runBB(t, "open", "--timeout", "5", "--raw", server.URL+"/")
		if !strings.Contains(out, "Test Page") {
			t.Errorf("expected page with timeout flag, got: %s", out)
		}
	})
}
