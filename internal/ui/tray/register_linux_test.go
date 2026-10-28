package tray

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// watcher stands in for the desktop's StatusNotifierWatcher: it passes on each item that registers.
type watcher struct {
	registered chan item
}

// item is a registered tray item: its bus name and object path.
type item struct {
	sender dbus.Sender
	path   string
}

func (w watcher) RegisterStatusNotifierItem(path string, sender dbus.Sender) *dbus.Error {
	w.registered <- item{sender: sender, path: path}
	return nil
}

// startWatcher owns the watcher's name on the bus at addr until the returned connection closes.
func startWatcher(t *testing.T, addr string, w watcher) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(w, "/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher"); err != nil {
		t.Fatal(err)
	}
	reply, err := conn.RequestName("org.kde.StatusNotifierWatcher", dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("could not own the watcher's name: %v %v", reply, err)
	}
	return conn
}

// privateBus starts a session bus of the test's own, so the desktop's is never touched.
func privateBus(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("needs dbus-daemon")
	}
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(addr)
}

func registration(t *testing.T, w watcher) item {
	t.Helper()
	select {
	case it := <-w.registered:
		return it
	case <-time.After(5 * time.Second):
		t.Fatal("the tray item did not register with the watcher")
		return item{}
	}
}

func property(t *testing.T, conn *dbus.Conn, it item, name string) dbus.Variant {
	t.Helper()
	v, err := conn.Object(string(it.sender), dbus.ObjectPath(it.path)).GetProperty("org.kde.StatusNotifierItem." + name)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// systray keeps its state for the process, so this is the package's only test that runs the tray.
func TestTheTrayItemRegistersWithItsLookAndAgainWhenTheWatcherRestarts(t *testing.T) {
	addr := privateBus(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	w := watcher{registered: make(chan item, 4)}
	first := startWatcher(t, addr, w)

	ctx, cancel := context.WithCancel(t.Context())
	ran := make(chan struct{})
	go func() {
		Run(ctx, &fakeApp{})
		close(ran)
	}()
	defer func() { cancel(); <-ran }()

	it := registration(t, w)
	if px := property(t, first, it, "IconPixmap").Value().([][]any); len(px) != 1 || px[0][0].(int32) != iconSize {
		t.Errorf("icon %v: want one %dpx image", px, iconSize)
	}
	if title := property(t, first, it, "Title").Value(); title != "Gold Forever Companion" {
		t.Errorf("title %q", title)
	}
	if tip := property(t, first, it, "ToolTip").Value().([]any); tip[2] != "Gold Forever Companion" {
		t.Errorf("tooltip %v", tip)
	}

	_ = first.Close()
	second := startWatcher(t, addr, w)
	defer second.Close()
	if again := registration(t, w); again.path != it.path {
		t.Errorf("registered again at %q, first at %q", again.path, it.path)
	}
}
