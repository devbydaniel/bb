package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	readability "github.com/go-shiori/go-readability"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
)

//go:embed help.txt
var helpText string

// State persisted between CLI invocations
type State struct {
	DebugURL   string `json:"debug_url"`
	ChromePID  int    `json:"chrome_pid"`
	ActivePage int    `json:"active_page"`
	DataDir    string `json:"data_dir"`
}

func stateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".bb")
}

func statePath() string {
	return filepath.Join(stateDir(), "state.json")
}

func loadState() (*State, error) {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("corrupt state file: %w", err)
	}
	return &s, nil
}

func saveState(s *State) error {
	if err := os.MkdirAll(stateDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), data, 0644)
}

func removeState() {
	_ = os.Remove(statePath())
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

// Default timeout for element queries
var defaultTimeout = 30 * time.Second

func init() {
	signal.Ignore(syscall.SIGPIPE)
	if t := os.Getenv("BB_TIMEOUT"); t != "" {
		if secs, err := strconv.ParseFloat(t, 64); err == nil {
			defaultTimeout = time.Duration(secs * float64(time.Second))
		}
	}
}

// ensureBrowser auto-starts Chrome if not running, returns state + connected browser
func ensureBrowser() (*State, *rod.Browser) {
	s, err := loadState()
	if err == nil {
		// Try connecting to existing browser
		browser := rod.New().ControlURL(s.DebugURL)
		if err := browser.Connect(); err == nil {
			return s, browser
		}
		// Stale state, clean up
		removeState()
	}

	// Start new browser
	dataDir := filepath.Join(stateDir(), "chrome-data")
	_ = os.MkdirAll(dataDir, 0755)

	l := launcher.New().
		Set("no-sandbox").
		Set("disable-gpu").
		Set("disable-dev-shm-usage").
		Set("password-store", "basic").
		Headless(true).
		Leakless(false).
		UserDataDir(dataDir)

	if bin := os.Getenv("BB_CHROME_BIN"); bin != "" {
		l = l.Bin(bin)
	}

	debugURL := l.MustLaunch()
	pid := l.PID()

	s = &State{
		DebugURL:   debugURL,
		ChromePID:  pid,
		ActivePage: 0,
		DataDir:    dataDir,
	}
	if err := saveState(s); err != nil {
		fatal("failed to save state: %v", err)
	}

	browser := rod.New().ControlURL(s.DebugURL)
	if err := browser.Connect(); err != nil {
		fatal("failed to connect to new browser: %v", err)
	}

	return s, browser
}

// withPage returns state, browser, and the active page (auto-starting if needed)
func withPage() (*State, *rod.Browser, *rod.Page) {
	s, browser := ensureBrowser()
	pages, err := browser.Pages()
	if err != nil {
		fatal("failed to list pages: %v", err)
	}
	if len(pages) == 0 {
		fatal("no pages open")
	}
	idx := s.ActivePage
	if idx < 0 || idx >= len(pages) {
		idx = 0
	}
	return s, browser, pages[idx].Timeout(defaultTimeout)
}

// extractReadableContent extracts readable text from HTML using go-readability
// with a timeout to avoid hanging on complex pages
func extractReadableContent(htmlContent string, pageURL string) (title string, content string, err error) {
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return "", "", err
	}

	type result struct {
		title   string
		content string
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		// go-readability can be slow on large pages, cap memory/time
		runtime.LockOSThread()
		article, err := readability.FromReader(strings.NewReader(htmlContent), parsedURL)
		if err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{title: article.Title, content: article.TextContent}
	}()

	select {
	case r := <-ch:
		return r.title, r.content, r.err
	case <-time.After(10 * time.Second):
		return "", "", fmt.Errorf("readability extraction timed out")
	}
}

// --- Global flags ---

type globalFlags struct {
	jsonOutput bool
	timeout    float64
}

func parseGlobalFlags(args []string) ([]string, globalFlags) {
	var flags globalFlags
	var remaining []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			flags.jsonOutput = true
		case "--timeout":
			i++
			if i >= len(args) {
				fatal("missing value for --timeout")
			}
			v, err := strconv.ParseFloat(args[i], 64)
			if err != nil {
				fatal("invalid timeout: %v", err)
			}
			flags.timeout = v
		default:
			remaining = append(remaining, args[i])
		}
	}
	if flags.timeout > 0 {
		defaultTimeout = time.Duration(flags.timeout * float64(time.Second))
	}
	return remaining, flags
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(helpText)
		os.Exit(1)
	}

	cmd := os.Args[1]
	args, flags := parseGlobalFlags(os.Args[2:])

	switch cmd {
	case "open":
		cmdOpen(args, flags)
	case "back":
		cmdBack()
	case "forward":
		cmdForward()
	case "reload":
		cmdReload()
	case "url":
		cmdURL()
	case "title":
		cmdTitle()
	case "text":
		cmdText(args)
	case "html":
		cmdHTML(args)
	case "attr":
		cmdAttr(args)
	case "pdf":
		cmdPDF(args)
	case "extract":
		cmdExtract(flags)
	case "js":
		cmdJS(args, flags)
	case "click":
		cmdClick(args)
	case "input":
		cmdInput(args)
	case "clear":
		cmdClear(args)
	case "select":
		cmdSelect(args)
	case "submit":
		cmdSubmit(args)
	case "hover":
		cmdHover(args)
	case "focus":
		cmdFocus(args)
	case "wait":
		cmdWait(args)
	case "waitload":
		cmdWaitLoad()
	case "waitstable":
		cmdWaitStable()
	case "waitidle":
		cmdWaitIdle()
	case "sleep":
		cmdSleep(args)
	case "screenshot":
		cmdScreenshot(args)
	case "screenshot-el":
		cmdScreenshotEl(args)
	case "pages":
		cmdPages(flags)
	case "page":
		cmdPage(args)
	case "newpage":
		cmdNewPage(args)
	case "closepage":
		cmdClosePage(args)
	case "exists":
		cmdExists(args)
	case "count":
		cmdCount(args)
	case "visible":
		cmdVisible(args)
	case "ax-tree":
		cmdAXTree(args, flags)
	case "ax-find":
		cmdAXFind(args, flags)
	case "ax-node":
		cmdAXNode(args, flags)
	case "status":
		cmdStatus(flags)
	case "stop":
		cmdStop()
	case "help", "-h", "--help":
		fmt.Print(helpText)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		fmt.Print(helpText)
		os.Exit(1)
	}
}

// --- Commands ---

func cmdOpen(args []string, flags globalFlags) {
	raw := false
	waitStable := false
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--raw":
			raw = true
		case "--wait":
			waitStable = true
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) < 1 {
		fatal("usage: bb open <url>")
	}
	u := positional[0]
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}

	s, browser := ensureBrowser()
	pages, _ := browser.Pages()
	var page *rod.Page
	if len(pages) == 0 {
		page = stealth.MustPage(browser)
		page = page.Timeout(defaultTimeout)
		if err := page.Navigate(u); err != nil {
			fatal("navigation failed: %v", err)
		}
		s.ActivePage = 0
		_ = saveState(s)
	} else {
		idx := s.ActivePage
		if idx < 0 || idx >= len(pages) {
			idx = 0
		}
		page = pages[idx].Timeout(defaultTimeout)
		if err := page.Navigate(u); err != nil {
			fatal("navigation failed: %v", err)
		}
	}
	page.MustWaitLoad()
	if waitStable {
		page.MustWaitStable()
	}

	info, _ := page.Info()
	currentURL := ""
	pageTitle := ""
	if info != nil {
		currentURL = info.URL
		pageTitle = info.Title
	}

	if raw {
		if flags.jsonOutput {
			out, _ := json.MarshalIndent(map[string]string{
				"url":   currentURL,
				"title": pageTitle,
			}, "", "  ")
			fmt.Println(string(out))
		} else {
			fmt.Println(pageTitle)
		}
		return
	}

	// Extract readable content
	html := page.MustEval(`() => document.documentElement.outerHTML`).Str()
	title, content, err := extractReadableContent(html, currentURL)
	if err != nil || strings.TrimSpace(content) == "" {
		// Fallback: get body innerText
		content = page.MustEval(`() => document.body?.innerText ?? ""`).Str()
	}
	if title == "" {
		title = pageTitle
	}

	// Truncate if very large (50KB limit for agent consumption)
	const maxBytes = 50 * 1024
	truncated := false
	if len(content) > maxBytes {
		content = content[:maxBytes]
		truncated = true
	}

	if flags.jsonOutput {
		out, _ := json.MarshalIndent(map[string]interface{}{
			"url":       currentURL,
			"title":     title,
			"content":   content,
			"truncated": truncated,
		}, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Printf("# %s\n\n%s", title, content)
		if truncated {
			fmt.Fprintf(os.Stderr, "\n[content truncated to 50KB]\n")
		}
	}
}

func cmdExtract(flags globalFlags) {
	_, _, page := withPage()
	info, _ := page.Info()
	currentURL := ""
	pageTitle := ""
	if info != nil {
		currentURL = info.URL
		pageTitle = info.Title
	}

	html := page.MustEval(`() => document.documentElement.outerHTML`).Str()
	title, content, err := extractReadableContent(html, currentURL)
	if err != nil || strings.TrimSpace(content) == "" {
		content = page.MustEval(`() => document.body?.innerText ?? ""`).Str()
	}
	if title == "" {
		title = pageTitle
	}

	const maxBytes = 50 * 1024
	truncated := false
	if len(content) > maxBytes {
		content = content[:maxBytes]
		truncated = true
	}

	if flags.jsonOutput {
		out, _ := json.MarshalIndent(map[string]interface{}{
			"url":       currentURL,
			"title":     title,
			"content":   content,
			"truncated": truncated,
		}, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Printf("# %s\n\n%s", title, content)
		if truncated {
			fmt.Fprintf(os.Stderr, "\n[content truncated to 50KB]\n")
		}
	}
}

func cmdBack() {
	_, _, page := withPage()
	page.MustNavigateBack()
	page.MustWaitLoad()
	info, _ := page.Info()
	if info != nil {
		fmt.Println(info.URL)
	}
}

func cmdForward() {
	_, _, page := withPage()
	page.MustNavigateForward()
	page.MustWaitLoad()
	info, _ := page.Info()
	if info != nil {
		fmt.Println(info.URL)
	}
}

func cmdReload() {
	_, _, page := withPage()
	page.MustReload()
	page.MustWaitLoad()
	fmt.Println("Reloaded")
}

func cmdURL() {
	_, _, page := withPage()
	info, err := page.Info()
	if err != nil {
		fatal("failed to get page info: %v", err)
	}
	fmt.Println(info.URL)
}

func cmdTitle() {
	_, _, page := withPage()
	info, err := page.Info()
	if err != nil {
		fatal("failed to get page info: %v", err)
	}
	fmt.Println(info.Title)
}

func cmdText(args []string) {
	_, _, page := withPage()
	if len(args) > 0 {
		el, err := page.Element(args[0])
		if err != nil {
			fatal("element not found: %v", err)
		}
		text, err := el.Text()
		if err != nil {
			fatal("failed to get text: %v", err)
		}
		fmt.Println(text)
	} else {
		// No selector: return body text
		text := page.MustEval(`() => document.body?.innerText ?? ""`).Str()
		fmt.Println(text)
	}
}

func cmdHTML(args []string) {
	_, _, page := withPage()
	if len(args) > 0 {
		el, err := page.Element(args[0])
		if err != nil {
			fatal("element not found: %v", err)
		}
		html, err := el.HTML()
		if err != nil {
			fatal("failed to get HTML: %v", err)
		}
		fmt.Println(html)
	} else {
		html := page.MustEval(`() => document.documentElement.outerHTML`).Str()
		fmt.Println(html)
	}
}

func cmdAttr(args []string) {
	if len(args) < 2 {
		fatal("usage: bb attr <selector> <attribute>")
	}
	_, _, page := withPage()
	el, err := page.Element(args[0])
	if err != nil {
		fatal("element not found: %v", err)
	}
	val := el.MustAttribute(args[1])
	if val == nil {
		fatal("attribute %q not found", args[1])
	}
	fmt.Println(*val)
}

func cmdPDF(args []string) {
	file := "page.pdf"
	if len(args) > 0 {
		file = args[0]
	}
	_, _, page := withPage()
	req := proto.PagePrintToPDF{}
	r, err := page.PDF(&req)
	if err != nil {
		fatal("failed to generate PDF: %v", err)
	}
	buf := make([]byte, 0)
	tmp := make([]byte, 32*1024)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	if err := os.WriteFile(file, buf, 0644); err != nil {
		fatal("failed to write PDF: %v", err)
	}
	fmt.Printf("Saved %s (%d bytes)\n", file, len(buf))
}

func cmdJS(args []string, flags globalFlags) {
	if len(args) < 1 {
		fatal("usage: bb js <expression>")
	}
	expr := strings.Join(args, " ")
	_, _, page := withPage()

	js := fmt.Sprintf(`() => { return (%s); }`, expr)
	result, err := page.Eval(js)
	if err != nil {
		fatal("JS error: %v", err)
	}

	v := result.Value
	raw := v.JSON("", "")

	if flags.jsonOutput {
		// Wrap in a result object
		fmt.Printf(`{"value":%s}`, raw)
		fmt.Println()
		return
	}

	switch {
	case raw == "null" || raw == "undefined":
		fmt.Println(raw)
	case raw == "true" || raw == "false":
		fmt.Println(raw)
	case len(raw) > 0 && raw[0] == '"':
		fmt.Println(v.Str())
	case len(raw) > 0 && (raw[0] == '{' || raw[0] == '['):
		fmt.Println(v.JSON("", "  "))
	default:
		fmt.Println(raw)
	}
}

func cmdClick(args []string) {
	if len(args) < 1 {
		fatal("usage: bb click <selector>")
	}
	_, _, page := withPage()
	el, err := page.Element(args[0])
	if err != nil {
		fatal("element not found: %v", err)
	}
	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		fatal("click failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	fmt.Println("Clicked")
}

func cmdInput(args []string) {
	if len(args) < 2 {
		fatal("usage: bb input <selector> <text>")
	}
	_, _, page := withPage()
	el, err := page.Element(args[0])
	if err != nil {
		fatal("element not found: %v", err)
	}
	text := strings.Join(args[1:], " ")
	el.MustSelectAllText().MustInput(text)
	fmt.Printf("Typed: %s\n", text)
}

func cmdClear(args []string) {
	if len(args) < 1 {
		fatal("usage: bb clear <selector>")
	}
	_, _, page := withPage()
	el, err := page.Element(args[0])
	if err != nil {
		fatal("element not found: %v", err)
	}
	el.MustSelectAllText().MustInput("")
	fmt.Println("Cleared")
}

func cmdSelect(args []string) {
	if len(args) < 2 {
		fatal("usage: bb select <selector> <value>")
	}
	_, _, page := withPage()
	js := fmt.Sprintf(`() => {
		const el = document.querySelector(%q);
		if (!el) throw new Error('element not found');
		el.value = %q;
		el.dispatchEvent(new Event('change', {bubbles: true}));
		return el.value;
	}`, args[0], args[1])
	result, err := page.Eval(js)
	if err != nil {
		fatal("select failed: %v", err)
	}
	fmt.Printf("Selected: %s\n", result.Value.Str())
}

func cmdSubmit(args []string) {
	if len(args) < 1 {
		fatal("usage: bb submit <selector>")
	}
	_, _, page := withPage()
	_, err := page.Element(args[0])
	if err != nil {
		fatal("form not found: %v", err)
	}
	page.MustEval(fmt.Sprintf(`() => document.querySelector(%q).submit()`, args[0]))
	fmt.Println("Submitted")
}

func cmdHover(args []string) {
	if len(args) < 1 {
		fatal("usage: bb hover <selector>")
	}
	_, _, page := withPage()
	el, err := page.Element(args[0])
	if err != nil {
		fatal("element not found: %v", err)
	}
	el.MustHover()
	fmt.Println("Hovered")
}

func cmdFocus(args []string) {
	if len(args) < 1 {
		fatal("usage: bb focus <selector>")
	}
	_, _, page := withPage()
	el, err := page.Element(args[0])
	if err != nil {
		fatal("element not found: %v", err)
	}
	el.MustFocus()
	fmt.Println("Focused")
}

func cmdWait(args []string) {
	if len(args) < 1 {
		fatal("usage: bb wait <selector>")
	}
	_, _, page := withPage()
	el, err := page.Element(args[0])
	if err != nil {
		fatal("element not found: %v", err)
	}
	el.MustWaitVisible()
	fmt.Println("Element visible")
}

func cmdWaitLoad() {
	_, _, page := withPage()
	page.MustWaitLoad()
	fmt.Println("Page loaded")
}

func cmdWaitStable() {
	_, _, page := withPage()
	page.MustWaitStable()
	fmt.Println("DOM stable")
}

func cmdWaitIdle() {
	_, _, page := withPage()
	page.MustWaitIdle()
	fmt.Println("Network idle")
}

func cmdSleep(args []string) {
	if len(args) < 1 {
		fatal("usage: bb sleep <seconds>")
	}
	secs, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		fatal("invalid seconds: %v", err)
	}
	time.Sleep(time.Duration(secs * float64(time.Second)))
}

// nextAvailableFile returns base+ext if it doesn't exist, otherwise base-2+ext, etc.
func nextAvailableFile(base, ext string) string {
	name := base + ext
	if _, err := os.Stat(name); os.IsNotExist(err) {
		return name
	}
	for i := 2; ; i++ {
		name = fmt.Sprintf("%s-%d%s", base, i, ext)
		if _, err := os.Stat(name); os.IsNotExist(err) {
			return name
		}
	}
}

func cmdScreenshot(args []string) {
	var file string
	width := 1280
	height := 0
	fullPage := true

	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-w", "--width":
			i++
			if i >= len(args) {
				fatal("missing value for %s", args[i-1])
			}
			v, err := strconv.Atoi(args[i])
			if err != nil {
				fatal("invalid width: %v", err)
			}
			width = v
		case "-h", "--height":
			i++
			if i >= len(args) {
				fatal("missing value for %s", args[i-1])
			}
			v, err := strconv.Atoi(args[i])
			if err != nil {
				fatal("invalid height: %v", err)
			}
			height = v
			fullPage = false
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) > 0 {
		file = positional[0]
	} else {
		file = nextAvailableFile("screenshot", ".png")
	}

	_, _, page := withPage()

	viewportHeight := height
	if viewportHeight == 0 {
		viewportHeight = 720
	}
	err := proto.EmulationSetDeviceMetricsOverride{
		Width:             width,
		Height:            viewportHeight,
		DeviceScaleFactor: 1,
	}.Call(page)
	if err != nil {
		fatal("failed to set viewport: %v", err)
	}

	data, err := page.Screenshot(fullPage, nil)
	if err != nil {
		fatal("screenshot failed: %v", err)
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		fatal("failed to write screenshot: %v", err)
	}
	fmt.Println(file)
}

func cmdScreenshotEl(args []string) {
	if len(args) < 1 {
		fatal("usage: bb screenshot-el <selector> [file]")
	}
	file := "element.png"
	if len(args) > 1 {
		file = args[1]
	}
	_, _, page := withPage()
	el, err := page.Element(args[0])
	if err != nil {
		fatal("element not found: %v", err)
	}
	data, err := el.Screenshot(proto.PageCaptureScreenshotFormatPng, 0)
	if err != nil {
		fatal("screenshot failed: %v", err)
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		fatal("failed to write screenshot: %v", err)
	}
	fmt.Printf("Saved %s (%d bytes)\n", file, len(data))
}

func cmdPages(flags globalFlags) {
	s, browser := ensureBrowser()
	pages, err := browser.Pages()
	if err != nil {
		fatal("failed to list pages: %v", err)
	}

	if flags.jsonOutput {
		type pageInfo struct {
			Index  int    `json:"index"`
			Active bool   `json:"active"`
			Title  string `json:"title"`
			URL    string `json:"url"`
		}
		var items []pageInfo
		for i, p := range pages {
			info, _ := p.Info()
			pi := pageInfo{Index: i, Active: i == s.ActivePage}
			if info != nil {
				pi.Title = info.Title
				pi.URL = info.URL
			}
			items = append(items, pi)
		}
		out, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(out))
		return
	}

	for i, p := range pages {
		marker := " "
		if i == s.ActivePage {
			marker = "*"
		}
		info, _ := p.Info()
		if info != nil {
			fmt.Printf("%s [%d] %s - %s\n", marker, i, info.Title, info.URL)
		} else {
			fmt.Printf("%s [%d] (unknown)\n", marker, i)
		}
	}
}

func cmdPage(args []string) {
	if len(args) < 1 {
		fatal("usage: bb page <index>")
	}
	idx, err := strconv.Atoi(args[0])
	if err != nil {
		fatal("invalid index: %v", err)
	}
	s, browser := ensureBrowser()
	pages, err := browser.Pages()
	if err != nil {
		fatal("failed to list pages: %v", err)
	}
	if idx < 0 || idx >= len(pages) {
		fatal("page index %d out of range (0-%d)", idx, len(pages)-1)
	}
	s.ActivePage = idx
	if err := saveState(s); err != nil {
		fatal("failed to save state: %v", err)
	}
	info, _ := pages[idx].Info()
	if info != nil {
		fmt.Printf("Switched to [%d] %s - %s\n", idx, info.Title, info.URL)
	}
}

func cmdNewPage(args []string) {
	s, browser := ensureBrowser()

	u := ""
	if len(args) > 0 {
		u = args[0]
		if !strings.Contains(u, "://") {
			u = "https://" + u
		}
	}

	var page *rod.Page
	page = stealth.MustPage(browser)
	if u != "" {
		if err := page.Navigate(u); err != nil {
			fatal("navigation failed: %v", err)
		}
		page.MustWaitLoad()
	}

	pages, _ := browser.Pages()
	for i, p := range pages {
		if p.TargetID == page.TargetID {
			s.ActivePage = i
			break
		}
	}
	_ = saveState(s)

	info, _ := page.Info()
	if info != nil {
		fmt.Printf("Opened [%d] %s\n", s.ActivePage, info.URL)
	}
}

func cmdClosePage(args []string) {
	s, browser := ensureBrowser()
	pages, err := browser.Pages()
	if err != nil {
		fatal("failed to list pages: %v", err)
	}
	if len(pages) <= 1 {
		fatal("cannot close the last page")
	}

	idx := s.ActivePage
	if len(args) > 0 {
		idx, err = strconv.Atoi(args[0])
		if err != nil {
			fatal("invalid index: %v", err)
		}
	}
	if idx < 0 || idx >= len(pages) {
		fatal("page index %d out of range", idx)
	}

	pages[idx].MustClose()
	if s.ActivePage >= len(pages)-1 {
		s.ActivePage = len(pages) - 2
	}
	if s.ActivePage < 0 {
		s.ActivePage = 0
	}
	_ = saveState(s)
	fmt.Printf("Closed page %d\n", idx)
}

func cmdExists(args []string) {
	if len(args) < 1 {
		fatal("usage: bb exists <selector>")
	}
	_, _, page := withPage()
	has, _, err := page.Has(args[0])
	if err != nil {
		fatal("query failed: %v", err)
	}
	if has {
		fmt.Println("true")
		os.Exit(0)
	} else {
		fmt.Println("false")
		os.Exit(1)
	}
}

func cmdCount(args []string) {
	if len(args) < 1 {
		fatal("usage: bb count <selector>")
	}
	_, _, page := withPage()
	els, err := page.Elements(args[0])
	if err != nil {
		fatal("query failed: %v", err)
	}
	fmt.Println(len(els))
}

func cmdVisible(args []string) {
	if len(args) < 1 {
		fatal("usage: bb visible <selector>")
	}
	_, _, page := withPage()
	el, err := page.Element(args[0])
	if err != nil {
		fmt.Println("false")
		os.Exit(1)
	}
	visible, err := el.Visible()
	if err != nil {
		fmt.Println("false")
		os.Exit(1)
	}
	if visible {
		fmt.Println("true")
	} else {
		fmt.Println("false")
		os.Exit(1)
	}
}

func cmdStatus(flags globalFlags) {
	s, err := loadState()
	if err != nil {
		if flags.jsonOutput {
			fmt.Println(`{"running": false}`)
		} else {
			fmt.Println("No active browser session")
		}
		return
	}
	browser := rod.New().ControlURL(s.DebugURL)
	if err := browser.Connect(); err != nil {
		if flags.jsonOutput {
			fmt.Println(`{"running": false, "stale": true}`)
		} else {
			fmt.Printf("Browser not responding (PID %d, state may be stale)\n", s.ChromePID)
		}
		return
	}
	pages, _ := browser.Pages()

	if flags.jsonOutput {
		type pageInfo struct {
			Index  int    `json:"index"`
			Active bool   `json:"active"`
			Title  string `json:"title"`
			URL    string `json:"url"`
		}
		var items []pageInfo
		for i, p := range pages {
			pi := pageInfo{Index: i, Active: i == s.ActivePage}
			if info, _ := p.Info(); info != nil {
				pi.Title = info.Title
				pi.URL = info.URL
			}
			items = append(items, pi)
		}
		out, _ := json.MarshalIndent(map[string]interface{}{
			"running":     true,
			"pid":         s.ChromePID,
			"pages":       items,
			"active_page": s.ActivePage,
		}, "", "  ")
		fmt.Println(string(out))
		return
	}

	fmt.Printf("Browser running (PID %d)\n", s.ChromePID)
	fmt.Printf("Pages: %d, Active: %d\n", len(pages), s.ActivePage)
	if page, err := getActivePage(browser, s); err == nil {
		if info, _ := page.Info(); info != nil {
			fmt.Printf("Current: %s - %s\n", info.Title, info.URL)
		}
	}
}

func getActivePage(browser *rod.Browser, s *State) (*rod.Page, error) {
	pages, err := browser.Pages()
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("no pages open")
	}
	idx := s.ActivePage
	if idx < 0 || idx >= len(pages) {
		idx = 0
	}
	return pages[idx], nil
}

func cmdStop() {
	s, err := loadState()
	if err != nil {
		fmt.Println("No active browser session")
		return
	}
	browser := rod.New().ControlURL(s.DebugURL)
	if err := browser.Connect(); err == nil {
		browser.MustClose()
	} else if s.ChromePID > 0 {
		if proc, err := os.FindProcess(s.ChromePID); err == nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
	}
	removeState()
	fmt.Println("Browser stopped")
}

// --- Accessibility commands ---

func cmdAXTree(args []string, flags globalFlags) {
	var depth *int
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--depth":
			i++
			if i >= len(args) {
				fatal("missing value for --depth")
			}
			v, err := strconv.Atoi(args[i])
			if err != nil {
				fatal("invalid depth: %v", err)
			}
			depth = &v
		default:
			fatal("unknown flag: %s", args[i])
		}
	}

	_, _, page := withPage()
	result, err := proto.AccessibilityGetFullAXTree{Depth: depth}.Call(page)
	if err != nil {
		fatal("failed to get accessibility tree: %v", err)
	}

	if flags.jsonOutput {
		data, _ := json.MarshalIndent(result.Nodes, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Print(formatAXTree(result.Nodes))
	}
}

func cmdAXFind(args []string, flags globalFlags) {
	var name, role string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			i++
			if i >= len(args) {
				fatal("missing value for --name")
			}
			name = args[i]
		case "--role":
			i++
			if i >= len(args) {
				fatal("missing value for --role")
			}
			role = args[i]
		default:
			fatal("unknown flag: %s", args[i])
		}
	}

	_, _, page := withPage()
	nodes, err := queryAXNodes(page, name, role)
	if err != nil {
		fatal("query failed: %v", err)
	}

	if len(nodes) == 0 {
		fmt.Fprintln(os.Stderr, "No matching nodes")
		os.Exit(1)
	}

	if flags.jsonOutput {
		data, _ := json.MarshalIndent(nodes, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Print(formatAXNodeList(nodes))
	}
}

func cmdAXNode(args []string, flags globalFlags) {
	if len(args) < 1 {
		fatal("usage: bb ax-node <selector>")
	}

	_, _, page := withPage()
	node, err := getAXNode(page, args[0])
	if err != nil {
		fatal("%v", err)
	}

	if flags.jsonOutput {
		data, _ := json.MarshalIndent(node, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Print(formatAXNodeDetail(node))
	}
}

// --- Accessibility helpers ---

func queryAXNodes(page *rod.Page, name, role string) ([]*proto.AccessibilityAXNode, error) {
	zero := 0
	doc, err := proto.DOMGetDocument{Depth: &zero}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("failed to get document: %w", err)
	}
	result, err := proto.AccessibilityQueryAXTree{
		BackendNodeID:  doc.Root.BackendNodeID,
		AccessibleName: name,
		Role:           role,
	}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("accessibility query failed: %w", err)
	}
	return result.Nodes, nil
}

func getAXNode(page *rod.Page, selector string) (*proto.AccessibilityAXNode, error) {
	el, err := page.Element(selector)
	if err != nil {
		return nil, fmt.Errorf("element not found: %w", err)
	}
	node, err := proto.DOMDescribeNode{ObjectID: el.Object.ObjectID}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("failed to describe DOM node: %w", err)
	}
	result, err := proto.AccessibilityGetPartialAXTree{
		BackendNodeID:  node.Node.BackendNodeID,
		FetchRelatives: false,
	}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("failed to get accessibility info: %w", err)
	}
	for _, n := range result.Nodes {
		if !n.Ignored {
			return n, nil
		}
	}
	if len(result.Nodes) > 0 {
		return result.Nodes[0], nil
	}
	return nil, fmt.Errorf("no accessibility node found for %q", selector)
}

func axValueStr(v *proto.AccessibilityAXValue) string {
	if v == nil {
		return ""
	}
	raw := v.Value.JSON("", "")
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		var s string
		if err := json.Unmarshal([]byte(raw), &s); err == nil {
			return s
		}
	}
	return raw
}

func formatAXTree(nodes []*proto.AccessibilityAXNode) string {
	if len(nodes) == 0 {
		return ""
	}

	nodeByID := make(map[proto.AccessibilityAXNodeID]*proto.AccessibilityAXNode)
	for _, n := range nodes {
		nodeByID[n.NodeID] = n
	}

	var rootID proto.AccessibilityAXNodeID
	for _, n := range nodes {
		if n.ParentID == "" {
			rootID = n.NodeID
			break
		}
	}
	if rootID == "" {
		rootID = nodes[0].NodeID
	}

	var sb strings.Builder
	var walk func(id proto.AccessibilityAXNodeID, depth int)
	walk = func(id proto.AccessibilityAXNodeID, depth int) {
		node, ok := nodeByID[id]
		if !ok {
			return
		}
		if !node.Ignored {
			indent := strings.Repeat("  ", depth)
			role := axValueStr(node.Role)
			name := axValueStr(node.Name)
			line := fmt.Sprintf("%s[%s]", indent, role)
			if name != "" {
				line += fmt.Sprintf(" %q", name)
			}
			props := formatProperties(node.Properties)
			if props != "" {
				line += " (" + props + ")"
			}
			sb.WriteString(line + "\n")
			for _, childID := range node.ChildIDs {
				walk(childID, depth+1)
			}
		} else {
			for _, childID := range node.ChildIDs {
				walk(childID, depth)
			}
		}
	}
	walk(rootID, 0)
	return sb.String()
}

func formatProperties(props []*proto.AccessibilityAXProperty) string {
	var parts []string
	for _, p := range props {
		val := axValueStr(p.Value)
		switch string(p.Name) {
		case "focusable", "disabled", "editable", "hidden", "required",
			"checked", "expanded", "selected", "modal", "multiline",
			"multiselectable", "readonly", "focused", "settable":
			if val == "true" {
				parts = append(parts, string(p.Name))
			}
		case "level":
			parts = append(parts, fmt.Sprintf("level=%s", val))
		case "autocomplete", "hasPopup", "orientation", "live",
			"relevant", "valuemin", "valuemax", "valuetext",
			"roledescription", "keyshortcuts":
			if val != "" {
				parts = append(parts, fmt.Sprintf("%s=%s", p.Name, val))
			}
		}
	}
	return strings.Join(parts, ", ")
}

func formatAXNodeList(nodes []*proto.AccessibilityAXNode) string {
	var sb strings.Builder
	for _, node := range nodes {
		role := axValueStr(node.Role)
		name := axValueStr(node.Name)
		line := fmt.Sprintf("[%s]", role)
		if name != "" {
			line += fmt.Sprintf(" %q", name)
		}
		if node.BackendDOMNodeID != 0 {
			line += fmt.Sprintf(" backendNodeId=%d", node.BackendDOMNodeID)
		}
		props := formatProperties(node.Properties)
		if props != "" {
			line += " (" + props + ")"
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

func formatAXNodeDetail(node *proto.AccessibilityAXNode) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("role: %s\n", axValueStr(node.Role)))
	if name := axValueStr(node.Name); name != "" {
		sb.WriteString(fmt.Sprintf("name: %s\n", name))
	}
	if desc := axValueStr(node.Description); desc != "" {
		sb.WriteString(fmt.Sprintf("description: %s\n", desc))
	}
	if val := axValueStr(node.Value); val != "" {
		sb.WriteString(fmt.Sprintf("value: %s\n", val))
	}
	for _, p := range node.Properties {
		val := axValueStr(p.Value)
		sb.WriteString(fmt.Sprintf("%s: %s\n", p.Name, val))
	}
	return sb.String()
}
