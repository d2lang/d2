package d2cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/d2lang/util-go/xmain"
	"github.com/d2lang/util-go/xos"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/urlenc"
)

func newTestServerHandler(t *testing.T, allowLocalFiles bool) http.Handler {
	t.Helper()
	opts := xmain.NewOpts(xos.NewEnv(nil), nil)
	populateLayoutOpts(&xmain.State{Opts: opts})
	localFiles := localfile.Policy{}
	if allowLocalFiles {
		localFiles = localfile.Unrestricted()
	}
	bundle := true
	asciiMode := "extended"
	srv := &diagramServer{
		ctx:             context.Background(),
		ms:              &xmain.State{Env: xos.NewEnv(nil), Opts: opts},
		localFiles:      localFiles,
		bundle:          bundle,
		asciiMode:       asciiMode,
		requestTimeout:  30 * time.Second,
		renderOpts:      d2svg.RenderOpts{},
		allowLocalFiles: allowLocalFiles,
	}
	return srv.handler()
}

func TestServeCmdRejectsExtraArguments(t *testing.T) {
	t.Parallel()
	opts := xmain.NewOpts(xos.NewEnv(nil), []string{"serve", "extra"})
	if err := opts.Flags.Parse(opts.Args); err != nil {
		t.Fatal(err)
	}
	err := serveCmd(context.Background(), &xmain.State{Opts: opts}, serveOpts{})
	var usageErr xmain.UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("serveCmd() error = %v, want xmain.UsageError", err)
	}
	const want = "bad usage: serve subcommand accepts no arguments"
	if err.Error() != want {
		t.Fatalf("serveCmd() error = %q, want %q", err, want)
	}
}

func TestServeRejectsWatchFlag(t *testing.T) {
	t.Parallel()
	state := &xmain.TestState{
		Run:  Run,
		Env:  xos.NewEnv(nil),
		Args: []string{"d2", "serve", "--watch"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state.Start(t, ctx)
	defer state.Cleanup(t)
	err := state.Wait(ctx)
	var usageErr xmain.UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("Run() error = %v, want xmain.UsageError", err)
	}
	if !strings.Contains(err.Error(), "cannot be combined with serve") {
		t.Fatalf("Run() error = %q, want watch rejection", err)
	}
}

func TestServeRejectsNegativeAnimateInterval(t *testing.T) {
	t.Parallel()
	state := &xmain.TestState{
		Run:  Run,
		Env:  xos.NewEnv(nil),
		Args: []string{"d2", "serve", "--animate-interval=-1"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state.Start(t, ctx)
	defer state.Cleanup(t)
	err := state.Wait(ctx)
	var usageErr xmain.UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("Run() error = %v, want xmain.UsageError", err)
	}
	if !strings.Contains(err.Error(), "--animate-interval must be non-negative") {
		t.Fatalf("Run() error = %q, want non-negative animate-interval message", err)
	}
}

func TestHelpDocumentsServeSubcommand(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	state := &xmain.TestState{
		Run:    Run,
		Env:    xos.NewEnv(nil),
		Args:   []string{"d2", "--help"},
		Stdout: &stdout,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state.Start(t, ctx)
	defer state.Cleanup(t)
	if err := state.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if output := stdout.String(); !strings.Contains(output, "serve") {
		t.Fatalf("help output does not mention serve subcommand:\n%s", output)
	}
}

func TestServeHandlerRenderFormatsAndOptions(t *testing.T) {
	t.Parallel()
	h := newTestServerHandler(t, false)

	encoded, err := urlenc.Encode("a -> b")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("GET svg with urlenc script", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/render/svg?script="+url.QueryEscape(encoded)+"&layout=elk&theme=200&sketch=1&no-xml-tag=1&salt=srv", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "image/svg+xml; charset=utf-8" {
			t.Fatalf("Content-Type = %q", got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q", got)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<svg") {
			t.Fatalf("unexpected SVG body: %s", body)
		}
		if strings.HasPrefix(body, "<?xml") {
			t.Fatal("expected XML declaration to be omitted with no-xml-tag=1")
		}

		// Verify salt changes the scoped SVG hash class.
		unsaltedReq := httptest.NewRequest(http.MethodGet, "/render/svg?script="+url.QueryEscape(encoded)+"&layout=elk&theme=200&sketch=1&no-xml-tag=1", nil)
		unsaltedRec := httptest.NewRecorder()
		h.ServeHTTP(unsaltedRec, unsaltedReq)
		if unsaltedRec.Code != http.StatusOK {
			t.Fatalf("unsalted status = %d, body = %s", unsaltedRec.Code, unsaltedRec.Body.String())
		}
		if unsaltedRec.Body.String() == body {
			t.Fatal("expected salt=srv to produce a distinct scoped SVG")
		}
	})

	t.Run("POST svg with raw D2 body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/render/svg?pad=0&center=1&scale=1&omit-version=1", strings.NewReader("alpha -> beta"))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<svg") || !strings.Contains(body, "alpha") || !strings.Contains(body, "beta") {
			t.Fatalf("unexpected SVG body: %s", body)
		}
	})

	t.Run("GET svg with ascii=1 returns text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/render/svg?script="+url.QueryEscape(encoded)+"&ascii=1", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Fatalf("Content-Type = %q", got)
		}
		if !strings.Contains(rec.Body.String(), "a") || !strings.Contains(rec.Body.String(), "b") {
			t.Fatalf("unexpected ASCII body: %q", rec.Body.String())
		}
	})

	t.Run("txt and ascii routes", func(t *testing.T) {
		for _, path := range []string{"/render/txt", "/render/ascii"} {
			req := httptest.NewRequest(http.MethodPost, path+"?ascii-mode=standard", strings.NewReader("a -> b"))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d, body = %s", path, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Fatalf("%s Content-Type = %q", path, got)
			}
			if !strings.Contains(rec.Body.String(), "a") {
				t.Fatalf("%s body = %q", path, rec.Body.String())
			}
		}
	})

	t.Run("png route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/render/png?scale=0.5", strings.NewReader("a -> b"))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "image/png" {
			t.Fatalf("Content-Type = %q", got)
		}
		if !bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
			t.Fatalf("response is not PNG: %x", rec.Body.Bytes()[:min(8, rec.Body.Len())])
		}
	})

	t.Run("gif pdf pptx and target", func(t *testing.T) {
		multi := "a -> b\nlayers: { detail: { x -> y } }\n"
		for _, tc := range []struct {
			path        string
			contentType string
			prefix      []byte
		}{
			{"/render/gif?animate-interval=200", "image/gif", []byte("GIF89a")},
			{"/render/pdf", "application/pdf", []byte("%PDF-")},
			{"/render/pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", []byte("PK")},
			{"/render/svg?target=", "image/svg+xml; charset=utf-8", []byte("<?xml")},
		} {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(multi))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d, body = %s", tc.path, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Fatalf("%s Content-Type = %q, want %q", tc.path, got, tc.contentType)
			}
			if !bytes.HasPrefix(rec.Body.Bytes(), tc.prefix) {
				t.Fatalf("%s missing prefix %q", tc.path, tc.prefix)
			}
		}
	})

	t.Run("theme names match numeric IDs", func(t *testing.T) {
		byIDReq := httptest.NewRequest(http.MethodPost, "/render/svg?theme=300&dark-theme=200", strings.NewReader("a -> b"))
		byIDRec := httptest.NewRecorder()
		h.ServeHTTP(byIDRec, byIDReq)
		if byIDRec.Code != http.StatusOK {
			t.Fatalf("by ID status = %d, body = %s", byIDRec.Code, byIDRec.Body.String())
		}

		byNameReq := httptest.NewRequest(http.MethodPost, "/render/svg?theme=Terminal&dark-theme=dark-mauve", strings.NewReader("a -> b"))
		byNameRec := httptest.NewRecorder()
		h.ServeHTTP(byNameRec, byNameReq)
		if byNameRec.Code != http.StatusOK {
			t.Fatalf("by name status = %d, body = %s", byNameRec.Code, byNameRec.Body.String())
		}
		if !bytes.Equal(byNameRec.Body.Bytes(), byIDRec.Body.Bytes()) {
			t.Fatal("rendered SVG by theme name differs from numeric theme ID")
		}
	})

	t.Run("dark-theme=-1 clears server default dark theme", func(t *testing.T) {
		opts := xmain.NewOpts(xos.NewEnv(nil), nil)
		populateLayoutOpts(&xmain.State{Opts: opts})
		darkThemeID := int64(200)
		withDefaultDark := (&diagramServer{
			ctx:            context.Background(),
			ms:             &xmain.State{Env: xos.NewEnv(nil), Opts: opts},
			bundle:         true,
			asciiMode:      "extended",
			requestTimeout: 30 * time.Second,
			renderOpts:     d2svg.RenderOpts{DarkThemeID: &darkThemeID},
		}).handler()

		plainReq := httptest.NewRequest(http.MethodPost, "/render/svg", strings.NewReader("a -> b"))
		plainRec := httptest.NewRecorder()
		h.ServeHTTP(plainRec, plainReq)
		if plainRec.Code != http.StatusOK {
			t.Fatalf("plain status = %d, body = %s", plainRec.Code, plainRec.Body.String())
		}

		defaultReq := httptest.NewRequest(http.MethodPost, "/render/svg", strings.NewReader("a -> b"))
		defaultRec := httptest.NewRecorder()
		withDefaultDark.ServeHTTP(defaultRec, defaultReq)
		if defaultRec.Code != http.StatusOK {
			t.Fatalf("default dark status = %d, body = %s", defaultRec.Code, defaultRec.Body.String())
		}
		if bytes.Equal(defaultRec.Body.Bytes(), plainRec.Body.Bytes()) {
			t.Fatal("expected server default dark-theme to affect SVG output")
		}

		clearedReq := httptest.NewRequest(http.MethodPost, "/render/svg?dark-theme=-1", strings.NewReader("a -> b"))
		clearedRec := httptest.NewRecorder()
		withDefaultDark.ServeHTTP(clearedRec, clearedReq)
		if clearedRec.Code != http.StatusOK {
			t.Fatalf("cleared dark status = %d, body = %s", clearedRec.Code, clearedRec.Body.String())
		}
		if !bytes.Equal(clearedRec.Body.Bytes(), plainRec.Body.Bytes()) {
			t.Fatal("expected dark-theme=-1 to clear server default dark-theme")
		}
	})

	t.Run("healthz", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
			t.Fatalf("healthz = %d %q", rec.Code, rec.Body.String())
		}
	})
}

func TestServeHandlerErrorsAndSecurity(t *testing.T) {
	t.Parallel()
	h := newTestServerHandler(t, false)

	encoded, err := urlenc.Encode("a -> b")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		method     string
		target     string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"missing script on GET", http.MethodGet, "/render/svg", "", http.StatusBadRequest, ""},
		{"bad script encoding", http.MethodGet, "/render/svg?script=not-valid-base64!!!", "", http.StatusBadRequest, ""},
		{"invalid D2 syntax", http.MethodPost, "/render/svg", "a -> {", http.StatusBadRequest, ""},
		{"unknown format", http.MethodGet, "/render/webp?script=" + url.QueryEscape(encoded), "", http.StatusNotFound, ""},
		{"empty render format", http.MethodGet, "/render/?script=" + url.QueryEscape(encoded), "", http.StatusNotFound, ""},
		{"unknown route", http.MethodGet, "/other", "", http.StatusNotFound, ""},
		{"method not allowed", http.MethodDelete, "/render/svg", "", http.StatusMethodNotAllowed, ""},
		{"unknown layout", http.MethodPost, "/render/svg?layout=unknown", "a", http.StatusBadRequest, ""},
		{"unknown theme", http.MethodPost, "/render/svg?theme=999999", "a", http.StatusBadRequest, "invalid theme parameter:"},
		{"invalid theme name", http.MethodPost, "/render/svg?theme=bogus", "a", http.StatusBadRequest, "invalid theme parameter:"},
		{"empty theme", http.MethodPost, "/render/svg?theme=", "a", http.StatusBadRequest, "invalid theme parameter:"},
		{"unknown dark-theme", http.MethodPost, "/render/svg?dark-theme=999999", "a", http.StatusBadRequest, "invalid dark-theme parameter:"},
		{"invalid dark-theme name", http.MethodPost, "/render/svg?dark-theme=bogus", "a", http.StatusBadRequest, "invalid dark-theme parameter:"},
		{"empty dark-theme", http.MethodPost, "/render/svg?dark-theme=", "a", http.StatusBadRequest, "invalid dark-theme parameter:"},
		{"invalid sketch bool", http.MethodPost, "/render/svg?sketch=maybe", "a", http.StatusBadRequest, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body = %q, want substring %q", rec.Body.String(), tc.wantBody)
			}
			if tc.wantStatus == http.StatusMethodNotAllowed && rec.Header().Get("Allow") == "" {
				t.Fatal("expected Allow header on 405 response")
			}
		})
	}

	t.Run("denies local file access by default", func(t *testing.T) {
		dir := t.TempDir()
		secretPath := filepath.Join(dir, "secret.d2")
		if err := os.WriteFile(secretPath, []byte("leaked_secret_node"), 0o600); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/render/svg", strings.NewReader("x: @"+secretPath))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "leaked_secret_node") {
			t.Fatal("local file content leaked in response")
		}
	})

	t.Run("allows local file access when explicitly enabled", func(t *testing.T) {
		trusted := newTestServerHandler(t, true)
		dir := t.TempDir()
		included := filepath.Join(dir, "part.d2")
		if err := os.WriteFile(included, []byte("trusted_node"), 0o600); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/render/svg", strings.NewReader("x: @"+included))
		rec := httptest.NewRecorder()
		trusted.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "trusted_node") {
			t.Fatalf("expected included node in SVG: %s", rec.Body.String())
		}
	})

	t.Run("rejects oversized request body", func(t *testing.T) {
		huge := bytes.Repeat([]byte("a"), int(maxServeScriptBytes)+1)
		req := httptest.NewRequest(http.MethodPost, "/render/svg", bytes.NewReader(huge))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
		}
	})
}

func TestServeCLILifecycleAndConcurrency(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "serve.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	state := &xmain.TestState{
		Run:    Run,
		PWD:    dir,
		Args:   []string{"d2", "serve", "--host=127.0.0.1", "--port=0", "--scale=1"},
		Env:    xos.NewEnv([]string{"PATH="}),
		Stderr: logFile,
	}
	state.Start(t, ctx)
	t.Cleanup(func() {
		cancel()
		shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		err := state.Wait(shutdown)
		if errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("serve failed to stop after context cancellation: %v", err)
			return
		}
		state.Cleanup(t)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("serve exited unexpectedly: %v", err)
		}
	})

	var baseURL string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(logPath)
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := scanner.Text()
			if idx := strings.Index(line, "listening on http://"); idx != -1 {
				baseURL = strings.TrimSpace(line[idx+len("listening on "):])
				break
			}
		}
		if baseURL != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if baseURL == "" {
		data, _ := os.ReadFile(logPath)
		t.Fatalf("serve did not log listening address:\n%s", string(data))
	}

	client := &http.Client{Timeout: 15 * time.Second}
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var resp *http.Response
			var reqErr error
			if idx%2 == 0 {
				resp, reqErr = client.Post(baseURL+"/render/svg", "text/plain", strings.NewReader("n1 -> n2"))
			} else {
				resp, reqErr = client.Post(baseURL+"/render/txt", "text/plain", strings.NewReader("n1 -> n2"))
			}
			if reqErr != nil {
				errs <- reqErr
				return
			}
			defer resp.Body.Close()
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				errs <- readErr
				return
			}
			if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("n1")) {
				errs <- errors.New("unexpected response: " + resp.Status + " " + string(body))
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for reqErr := range errs {
		t.Fatal(reqErr)
	}

	// Default serve mode must reject local-file imports unless --allow-local-files is set.
	secretPath := filepath.Join(dir, "secret.d2")
	if err := os.WriteFile(secretPath, []byte("secret_node"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(baseURL+"/render/svg", "text/plain", strings.NewReader("x: @"+secretPath))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("default serve local import status = %d, want 400", resp.StatusCode)
	}
}
