package instance

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// running stands in for a companion's settings server: it answers a ping's nonce with its proof.
func running(t *testing.T, secret string) Info {
	t.Helper()
	return listening(t, secret, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ping" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"version": "v0.1.0", "proof": Proof(secret, r.URL.Query().Get("nonce"))})
	})
}

func listening(t *testing.T, secret string, handler http.HandlerFunc) Info {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return Info{Port: n, Secret: secret, Version: "v0.1.0"}
}

func TestTheFileIsTheOwnersOnlyAndReadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.json")
	want := Info{PID: 42, Port: 50123, Secret: "s3cret", Version: "v0.1.0"}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v: the file holds the page's secret", info.Mode().Perm())
	}
	if got, err := Read(path); err != nil || got != want {
		t.Errorf("Read = %+v, %v", got, err)
	}
}

func TestAnInstanceThatAnswersItsSecretIsRunning(t *testing.T) {
	info := running(t, "right")
	if !Alive(info) {
		t.Error("a companion that answers its ping is running")
	}
	info.Secret = "wrong"
	if Alive(info) {
		t.Error("whatever answers on that port without the secret is not this companion")
	}
}

// After a crash the file stays; another program may later listen on its port and answer anything.
func TestAnotherProgramOnTheSamePortIsNotTheCompanionAndNeverSeesTheSecret(t *testing.T) {
	var seen []string
	info := listening(t, "s3cret", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.String()+" "+r.Header.Get(SecretHeader))
		w.Write([]byte(`<!doctype html><title>a dev server</title>`))
	})

	if Alive(info) {
		t.Error("a program that answers 200 to anything was taken for the running companion")
	}
	for _, s := range seen {
		if strings.Contains(s, "s3cret") {
			t.Errorf("the page's secret was sent to it: %s", s)
		}
	}
}

func TestAStaleFileIsNotARunningInstance(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close() // nothing listens there any more: the companion that wrote the file has gone
	if Alive(Info{Port: port, Secret: "x"}) {
		t.Error("a port nobody listens on is not a running instance")
	}
}
