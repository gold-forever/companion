package instance

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// running stands in for a companion's settings server: it answers a ping that carries its secret.
func running(t *testing.T, secret string) Info {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ping" || r.Header.Get(SecretHeader) != secret {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"version":"v0.1.0"}`))
	}))
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

func TestAStaleFileIsNotARunningInstance(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close() // nothing listens there any more: the companion that wrote the file has gone
	if Alive(Info{Port: port, Secret: "x"}) {
		t.Error("a port nobody listens on is not a running instance")
	}
}
