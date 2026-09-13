package d2cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewWatchAccessToken(t *testing.T) {
	t.Parallel()

	first, err := newWatchAccessToken()
	if err != nil {
		t.Fatalf("newWatchAccessToken() error = %v", err)
	}
	second, err := newWatchAccessToken()
	if err != nil {
		t.Fatalf("newWatchAccessToken() error = %v", err)
	}
	if first == second {
		t.Fatal("two watch access tokens unexpectedly matched")
	}
	if strings.ContainsAny(first, "+/=") {
		t.Fatalf("watch access token %q is not URL-safe", first)
	}
}

func TestWatcherAuthorizeRequest(t *testing.T) {
	t.Parallel()

	const (
		token      = "correct-token"
		cookieName = "d2-watch-12345"
	)
	w := &watcher{accessToken: token, accessCookieName: cookieName}

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://attacker.example/private-board", nil)
		recorder := httptest.NewRecorder()
		if w.authorizeRequest(recorder, req) {
			t.Fatal("authorizeRequest() accepted an unauthenticated request")
		}
		if got := recorder.Code; got != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", got, http.StatusForbidden)
		}
	})

	t.Run("rejects invalid cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://localhost/private-board", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: "wrong-token"})
		recorder := httptest.NewRecorder()
		if w.authorizeRequest(recorder, req) {
			t.Fatal("authorizeRequest() accepted an invalid cookie")
		}
		if got := recorder.Code; got != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", got, http.StatusForbidden)
		}
	})

	t.Run("exchanges token for strict HTTP-only cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://localhost/private-board?keep=1&token="+url.QueryEscape(token), nil)
		recorder := httptest.NewRecorder()
		if w.authorizeRequest(recorder, req) {
			t.Fatal("authorizeRequest() served the token-bearing URL without redirecting")
		}
		if got := recorder.Code; got != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", got, http.StatusSeeOther)
		}
		if got := recorder.Header().Get("Location"); got != "/private-board?keep=1" {
			t.Fatalf("Location = %q, want token-free board URL", got)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control = %q, want no-store", got)
		}
		if got := recorder.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
		}
		cookies := recorder.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("Set-Cookie count = %d, want 1", len(cookies))
		}
		cookie := cookies[0]
		if cookie.Name != cookieName || cookie.Value != token || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
			t.Fatalf("access cookie = %#v", cookie)
		}

		followup := httptest.NewRequest(http.MethodGet, "http://localhost/private-board", nil)
		followup.AddCookie(cookie)
		if !w.authorizeRequest(httptest.NewRecorder(), followup) {
			t.Fatal("authorizeRequest() rejected the exchanged access cookie")
		}
	})

	t.Run("strips repeated token URL despite valid cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://localhost/private-board?token="+url.QueryEscape(token), nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		recorder := httptest.NewRecorder()
		if w.authorizeRequest(recorder, req) {
			t.Fatal("authorizeRequest() served a token-bearing URL through the existing cookie")
		}
		if got := recorder.Code; got != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", got, http.StatusSeeOther)
		}
		if got := recorder.Header().Get("Location"); got != "/private-board" {
			t.Fatalf("Location = %q, want token-free board URL", got)
		}
	})
}
