package main

import (
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/platform/install"
	"github.com/gold-forever/companion/internal/platform/instance"
)

// olderCopyEnv makes the test binary stand in for v0.1.0-rc.12 running from the data folder it names.
const olderCopyEnv = "GFC_TEST_OLDER_COPY"

func TestMain(m *testing.M) {
	if dataDir := os.Getenv(olderCopyEnv); dataDir != "" {
		runAsOlderCopy(dataDir)
		return
	}
	os.Exit(m.Run())
}

// runAsOlderCopy is v0.1.0-rc.12 as far as another copy sees it: no claim, an instance file, a page
// that wants the secret on every request and answers a ping with its version only. Asked to quit, it
// finishes its upload, writes "uploaded" into dataDir, closes its page and exits a little later.
func runAsOlderCopy(dataDir string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(3)
	}
	secret := hex.EncodeToString([]byte("rc.12's secret"))
	info := instance.Info{PID: os.Getpid(), Port: l.Addr().(*net.TCPAddr).Port, Secret: secret, Version: "v0.1.0-rc.12"}
	quit := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(instance.SecretHeader) != secret {
			http.Error(w, "open the settings from the tray icon", http.StatusForbidden)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/ping":
			json.NewEncoder(w).Encode(map[string]string{"version": info.Version})
		case "POST /api/quit":
			w.Write([]byte(`{"ok":true}`))
			go close(quit)
		default:
			http.NotFound(w, r)
		}
	})}
	go srv.Serve(l)
	if instance.Write(filepath.Join(dataDir, "instance.json"), info) != nil {
		os.Exit(3)
	}
	<-quit
	time.Sleep(300 * time.Millisecond) // the upload under way
	_ = os.WriteFile(filepath.Join(dataDir, "uploaded"), nil, 0o600)
	srv.Close()
	time.Sleep(300 * time.Millisecond) // its page closed, the process has not yet exited
	os.Exit(0)
}

// startOlderCopy runs the stand-in for rc.12 from a binary named as the companion, and returns what its
// instance file records.
func startOlderCopy(t *testing.T, dataDir string) instance.Info {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "gold-forever-companion")
	if err := os.WriteFile(exe, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), olderCopyEnv+"="+dataDir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	t.Cleanup(func() { cmd.Process.Kill(); <-exited })
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if info, err := instance.Read(filepath.Join(dataDir, "instance.json")); err == nil {
			return info
		}
	}
	t.Fatal("the older copy wrote no instance file")
	return instance.Info{}
}

// installedScript stands in for the download: started, it writes its arguments and whether the copy
// with pid still ran into started. A copy that has exited but not yet been waited for has no exe link.
func installedScript(started string, pid int) string {
	return "#!/bin/sh\n" +
		"if [ -e /proc/" + strconv.Itoa(pid) + "/exe ]; then copy=running; else copy=exited; fi\n" +
		"printf '%s\\n%s\\n' \"$*\" \"$copy\" > '" + started + ".tmp' && mv '" + started + ".tmp' '" + started + "'\n"
}

// v0.1.0-rc.12 was running when v0.2.0-rc.1 was installed over it, and both kept running
// (gold-forever/companion#26). The download must see it, ask it to quit, and install only once it has
// exited, its upload finished.
func TestADownloadStopsARunningOlderCopyAndInstallsOnceItHasExited(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the copy's process is checked by its name in /proc")
	}
	was := version
	version = "v0.2.0-rc.1"
	t.Cleanup(func() { version = was })
	home := t.TempDir()
	dataDir := filepath.Join(home, "data")
	other := startOlderCopy(t, dataDir)
	l := install.For(install.Env{GOOS: "linux", Home: home, Getenv: func(string) string { return "" }})
	started := filepath.Join(home, "started")
	download := filepath.Join(t.TempDir(), "gold-forever-companion-linux-amd64")
	if err := os.WriteFile(download, []byte(installedScript(started, other.PID)), 0o755); err != nil {
		t.Fatal(err)
	}

	alive := instance.Alive(t.Context(), other)
	if !alive {
		t.Fatal("the running rc.12 copy was not seen, so the download would install beside it")
	}
	if err := installFromDownload(t.Context(), l, download, other, alive); err != nil {
		t.Fatal(err)
	}

	var report []byte
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if data, err := os.ReadFile(started); err == nil {
			report = data
			break
		}
	}
	args, copyState, _ := strings.Cut(string(report), "\n")
	if args != "--installed" {
		t.Fatalf("the installed copy was started with %q, want --installed", args)
	}
	if copyState != "exited\n" {
		t.Fatal("the download was installed while the older copy still ran")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "uploaded")); err != nil {
		t.Error("the older copy did not finish its upload before it exited")
	}
}
