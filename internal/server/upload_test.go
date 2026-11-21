package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// server answers every request with status and body, and keeps the last request it saw.
func server(t *testing.T, status int, body string, headers map[string]string) (*Client, *http.Request, *[]byte) {
	t.Helper()
	var seen http.Request
	var sent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r.Clone(context.Background())
		sent, _ = io.ReadAll(r.Body)
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Token: "gf_secret", Version: "0.0.1", OS: "linux"}, &seen, &sent
}

func TestUploadSendsTheSaveAsItIsWithTheToken(t *testing.T) {
	c, seen, sent := server(t, 200, `{"status":"imported","problems":2,"saves":7}`, nil)

	answer, err := c.Upload(context.Background(), []byte("GoldForeverDB = {}"))

	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if answer.Status != "imported" || answer.Problems != 2 || answer.Saves != 7 {
		t.Errorf("answer = %+v", answer)
	}
	if seen.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("content-encoding = %q", seen.Header.Get("Content-Encoding"))
	}
	if got := gunzip(t, *sent); got != "GoldForeverDB = {}" {
		t.Errorf("body = %q", got)
	}
	checks := map[string]string{
		"method":        seen.Method,
		"path":          seen.URL.Path,
		"name":          seen.URL.Query().Get("name"),
		"authorization": seen.Header.Get("Authorization"),
		"content-type":  seen.Header.Get("Content-Type"),
		"user-agent":    seen.Header.Get("User-Agent"),
	}
	want := map[string]string{
		"method":        "POST",
		"path":          "/api/v1/saves",
		"name":          "GoldForever.lua",
		"authorization": "Bearer gf_secret",
		"content-type":  "application/octet-stream",
		"user-agent":    "gold-forever-companion/0.0.1 (linux)",
	}
	for k, w := range want {
		if checks[k] != w {
			t.Errorf("%s = %q, want %q", k, checks[k], w)
		}
	}
}

func gunzip(t *testing.T, body []byte) string {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return string(raw)
}

// A save compresses about tenfold, so a slow uplink sends it in a tenth of the time (gf#410).
func TestALargeSaveIsSentCompressed(t *testing.T) {
	c, _, sent := server(t, 200, `{"status":"imported","saves":1}`, nil)
	save := []byte(strings.Repeat(`["Player-4184-0A1B2C3D"] = { ["money"] = 1234567, },`+"\n", 100000))

	if _, err := c.Upload(context.Background(), save); err != nil {
		t.Fatalf("upload: %v", err)
	}

	if len(*sent)*5 > len(save) {
		t.Errorf("sent %d bytes of a %d-byte save", len(*sent), len(save))
	}
}

// A server from before gzip uploads refuses a compressed body with 415; it gets the save again as it is.
func TestAServerThatRefusesGzipGetsTheSaveUncompressed(t *testing.T) {
	var encodings []string
	var last []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encodings = append(encodings, r.Header.Get("Content-Encoding"))
		last, _ = io.ReadAll(r.Body)
		if r.Header.Get("Content-Encoding") != "" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			_, _ = io.WriteString(w, `{"status":"unsupported media type","reason":"send the body uncompressed"}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"imported","saves":1}`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Token: "gf_secret", Version: "0.0.1", OS: "linux"}

	answer, err := c.Upload(context.Background(), []byte("GoldForeverDB = {}"))

	if err != nil || answer.Status != "imported" {
		t.Fatalf("answer = %+v, err = %v", answer, err)
	}
	if strings.Join(encodings, ",") != "gzip," || string(last) != "GoldForeverDB = {}" {
		t.Errorf("encodings = %q, last body = %q", encodings, last)
	}
}

func TestAnAlreadyImportedSaveIsNotAnError(t *testing.T) {
	c, _, _ := server(t, 200, `{"status":"already imported","saves":7}`, nil)

	answer, err := c.Upload(context.Background(), []byte("x"))

	if err != nil || answer.Status != "already imported" {
		t.Fatalf("answer = %+v, err = %v", answer, err)
	}
}

func TestARevokedTokenStopsUploads(t *testing.T) {
	c, _, _ := server(t, 401, `{"status":"unauthorized"}`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestAForbiddenAccountStopsUploadsWithTheReason(t *testing.T) {
	c, _, _ := server(t, 403, `{"status":"this account is suspended; uploads are refused"}`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	var forbidden *ForbiddenError
	if !errors.As(err, &forbidden) || forbidden.Reason != "this account is suspended; uploads are refused" {
		t.Fatalf("err = %v, want a ForbiddenError with the reason", err)
	}
	if Retryable(err) {
		t.Fatal("a forbidden upload must not be retried on its own")
	}
}

func TestASaveTheServerRefusesCarriesItsReason(t *testing.T) {
	for _, status := range []int{400, 422} {
		c, _, _ := server(t, status, `{"status":"unknown game","reason":"unknown game version 30403"}`, nil)

		_, err := c.Upload(context.Background(), []byte("x"))

		var refused *RefusedError
		if !errors.As(err, &refused) || refused.Status != status || refused.Reason != "unknown game version 30403" {
			t.Errorf("%d: err = %v", status, err)
		}
	}
}

func TestATooLargeSaveIsNotRetriedAndCarriesTheLimit(t *testing.T) {
	c, _, _ := server(t, 413, `{"status":"too large","reason":"a forever save may be at most 10 MB"}`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	var tooLarge *TooLargeError
	if !errors.Is(err, ErrTooLarge) || Retryable(err) || !errors.As(err, &tooLarge) || tooLarge.Reason != "a forever save may be at most 10 MB" {
		t.Fatalf("err = %v, retryable = %v", err, Retryable(err))
	}
}

func TestRateLimitedWaitsForRetryAfter(t *testing.T) {
	c, _, _ := server(t, 429, ``, map[string]string{"Retry-After": "120"})

	_, err := c.Upload(context.Background(), []byte("x"))

	var limited *RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != 120*time.Second || !Retryable(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestRateLimitedWithoutRetryAfterWaitsAMinute(t *testing.T) {
	c, _, _ := server(t, 429, ``, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	var limited *RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != time.Minute {
		t.Fatalf("err = %v", err)
	}
}

func TestAServerErrorIsRetried(t *testing.T) {
	c, _, _ := server(t, 502, `bad gateway`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	if err == nil || !Retryable(err) {
		t.Fatalf("err = %v, retryable = %v", err, Retryable(err))
	}
}

func TestAnUnreachableServerIsRetried(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", Token: "t", Version: "0", OS: "linux"}

	_, err := c.Upload(context.Background(), []byte("x"))

	if err == nil || !Retryable(err) {
		t.Fatalf("err = %v, retryable = %v", err, Retryable(err))
	}
}

func TestTheBaseURLMayEndWithASlash(t *testing.T) {
	c, seen, _ := server(t, 200, `{"status":"imported"}`, nil)
	c.BaseURL += "/"

	if _, err := c.Upload(context.Background(), []byte("x")); err != nil {
		t.Fatal(err)
	}
	if seen.URL.Path != "/api/v1/saves" {
		t.Errorf("path = %q", seen.URL.Path)
	}
}

// A proxy or ingress mid-deploy, or a wrong url in the settings, is not the server's last word on a save.
func TestAnUnexpectedClientErrorIsRetried(t *testing.T) {
	for _, status := range []int{404, 405, 408, 409, 421} {
		c, _, _ := server(t, status, `<html>not here</html>`, nil)

		_, err := c.Upload(context.Background(), []byte("x"))

		var refused *RefusedError
		if !Retryable(err) || errors.As(err, &refused) {
			t.Errorf("%d: err = %v, retryable = %v", status, err, Retryable(err))
		}
	}
}

func TestAWrongContentTypeIsRefusedForGood(t *testing.T) {
	c, _, _ := server(t, 415, `{"status":"send the file as application/octet-stream"}`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	var refused *RefusedError
	if !errors.As(err, &refused) || Retryable(err) {
		t.Fatalf("err = %v", err)
	}
}

// A captive portal or a proxy answers 200 too; only the server's own answer means the save arrived.
func TestA200ThatIsNotTheServersAnswerIsRetried(t *testing.T) {
	for _, body := range []string{`<html>portal</html>`, `{"status":"maybe"}`, ``} {
		c, _, _ := server(t, 200, body, nil)

		_, err := c.Upload(context.Background(), []byte("x"))

		if !Retryable(err) {
			t.Errorf("%q: err = %v, retryable = %v", body, err, Retryable(err))
		}
	}
}

// While the operator pauses uploads the server answers 503 with Retry-After and why.
func TestAPausedServerSaysHowLongToWaitAndWhy(t *testing.T) {
	c, _, _ := server(t, 503, `{"status":"paused","reason":"maintenance until 20:00"}`, map[string]string{"Retry-After": "600"})

	_, err := c.Upload(context.Background(), []byte("x"))

	if !Retryable(err) || RetryAfter(err) != 600*time.Second || !strings.Contains(err.Error(), "maintenance until 20:00") {
		t.Fatalf("err = %v, retryable = %v, retry after %s", err, Retryable(err), RetryAfter(err))
	}
}

func TestAServerErrorWithoutRetryAfterLeavesTheWaitToTheClient(t *testing.T) {
	c, _, _ := server(t, 502, `bad gateway`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	if RetryAfter(err) != 0 {
		t.Fatalf("retry after %s, want none", RetryAfter(err))
	}
}

// A 14 MB retail save needs 0.95 Mbit/s to go up in two minutes; a slower uplink must still get there.
func TestALargeSaveOnASlowUplinkIsGivenTimeToArrive(t *testing.T) {
	c := &Client{BaseURL: "https://gold-forever.app"}
	if timeout := c.uploadClient().Timeout; timeout != 0 {
		t.Errorf("an upload has a whole-request timeout of %s", timeout)
	}
	if got := uploadDeadline(32 << 20); got < 30*time.Minute {
		t.Errorf("a 32 MiB save gets %s", got)
	}
	if got := uploadDeadline(1 << 10); got != 2*time.Minute {
		t.Errorf("a small save gets %s, want the two minutes an answer may take", got)
	}
	if got := uploadTransport.ResponseHeaderTimeout; got != 2*time.Minute {
		t.Errorf("the server's answer may take %s", got)
	}
}

func TestTheWaitStopsGrowingAtThirtyMinutes(t *testing.T) {
	waits := []time.Duration{}
	var w time.Duration
	for range 8 {
		w = NextWait(w)
		waits = append(waits, w)
	}
	if waits[0] != time.Minute || waits[5] != 30*time.Minute || waits[7] != 30*time.Minute {
		t.Fatalf("waits = %v", waits)
	}
}

// decodeRealmFolders reads RealmFoldersHeader's value as the server does: percent-decoded, then JSON.
func decodeRealmFolders(t *testing.T, value string) map[string][]string {
	t.Helper()
	js, err := url.PathUnescape(value)
	if err != nil {
		t.Fatalf("not percent-encoded: %v", err)
	}
	var realms map[string][]string
	if err := json.Unmarshal([]byte(js), &realms); err != nil {
		t.Fatalf("not a JSON object of realms: %v (%s)", err, js)
	}
	return realms
}

func TestRealmFoldersEncodeAsAPercentEncodedJSONObjectOfRealms(t *testing.T) {
	folders := [][2]string{{"Kel'Thuzad", "Thrallmär"}, {"CAMWOW EU4 Realm 7 PvE", "Testchar"}, {"CAMWOW EU4 Realm 7 PvE", "Alt"}}

	value, err := EncodeRealmFolders(folders)

	if err != nil {
		t.Fatal(err)
	}
	want := "%7B%22CAMWOW%20EU4%20Realm%207%20PvE%22%3A%5B%22Alt%22%2C%22Testchar%22%5D%2C%22Kel%27Thuzad%22%3A%5B%22Thrallm%C3%A4r%22%5D%7D"
	if value != want {
		t.Fatalf("value = %s, want %s", value, want)
	}
	got := decodeRealmFolders(t, value)
	if !reflect.DeepEqual(got, map[string][]string{"CAMWOW EU4 Realm 7 PvE": {"Alt", "Testchar"}, "Kel'Thuzad": {"Thrallmär"}}) {
		t.Fatalf("round trip = %q", got)
	}
}

func TestRealmFoldersNameEachRealmOnceWithItsCharactersSortedAndEachOnce(t *testing.T) {
	folders := [][2]string{{"B", "Zed"}, {"A", "Yan"}, {"B", "Abe"}, {"A", "Yan"}, {"B", "Zed"}}

	value, _ := EncodeRealmFolders(folders)

	js, _ := url.PathUnescape(value)
	if want := `{"A":["Yan"],"B":["Abe","Zed"]}`; js != want {
		t.Fatalf("json = %s, want %s", js, want)
	}
}

// folders is n distinct characters on realm "R", each name prefix and two letters.
func folders(n int, prefix string) [][2]string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	out := make([][2]string, n)
	for i := range out {
		out[i] = [2]string{"R", prefix + string(letters[i/len(letters)]) + string(letters[i%len(letters)])}
	}
	return out
}

func TestRealmFoldersPast6KBAreNone(t *testing.T) {
	// Each character is 11 bytes encoded, with its quotes and comma; the object around them 19 bytes more.
	fits, over := folders(556, ""), folders(557, "")
	if v, err := EncodeRealmFolders(fits); err != nil || len(v) != 6135 {
		t.Fatalf("556 characters = %d bytes, %v; want 6135, fitting", len(v), err)
	}
	if v, err := EncodeRealmFolders(over); !errors.Is(err, ErrTooManyRealmFolders) || v != "" {
		t.Fatalf("557 characters = %d bytes, %v; want none", len(v), err)
	}
	// There is no cap on how many: repeats count once, so 557 pairs of which two are the same fit.
	if _, err := EncodeRealmFolders(append(fits, [2]string{"R", "aa"})); err != nil {
		t.Fatalf("556 distinct characters: %v", err)
	}
}

func TestNoRealmFoldersAreNoValue(t *testing.T) {
	if v, err := EncodeRealmFolders(nil); v != "" || err != nil {
		t.Fatalf("no pairs = %q, %v", v, err)
	}
}

func TestAnUploadCarriesTheRealmFoldersBesideTheSave(t *testing.T) {
	c, seen, _ := server(t, 200, `{"status":"imported","saves":1}`, nil)

	if _, err := c.UploadWithFolders(context.Background(), []byte("x"), [][2]string{{"Kel'Thuzad", "Thrallmär"}}); err != nil {
		t.Fatalf("upload: %v", err)
	}

	got := decodeRealmFolders(t, seen.Header.Get("X-GF-Realm-Folders"))
	if !reflect.DeepEqual(got, map[string][]string{"Kel'Thuzad": {"Thrallmär"}}) {
		t.Fatalf("header pairs = %q", got)
	}
}

func TestAnUploadWithoutRealmFoldersOrOver6KBHasNoHeaderAndStillGoes(t *testing.T) {
	for name, pairs := range map[string][][2]string{"none": nil, "over 6 KB": folders(557, "")} {
		c, seen, sent := server(t, 200, `{"status":"imported","saves":1}`, nil)

		if _, err := c.UploadWithFolders(context.Background(), []byte("x"), pairs); err != nil {
			t.Fatalf("%s: upload: %v", name, err)
		}

		if _, ok := seen.Header["X-Gf-Realm-Folders"]; ok {
			t.Errorf("%s: header sent: %q", name, seen.Header.Get("X-GF-Realm-Folders"))
		}
		if len(*sent) == 0 {
			t.Errorf("%s: no save sent", name)
		}
	}
}

// A server from before gzip gets the save again uncompressed, and the realm folders with it.
func TestTheUncompressedRetryCarriesTheRealmFoldersToo(t *testing.T) {
	var headers []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Get(RealmFoldersHeader))
		if r.Header.Get("Content-Encoding") == "gzip" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		_, _ = io.WriteString(w, `{"status":"imported","saves":1}`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Token: "gf_t", Version: "0.0.1", OS: "linux"}

	if _, err := c.UploadWithFolders(context.Background(), []byte("x"), [][2]string{{"R", "C"}}); err != nil {
		t.Fatalf("upload: %v", err)
	}

	if len(headers) != 2 || headers[0] == "" || headers[1] != headers[0] {
		t.Fatalf("headers = %q", headers)
	}
}
