//go:build linux

package media

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

var errCancelled = errors.New("udostępnianie ekranu anulowane")

// startScreenCast: Wayland screen sharing the way browsers do it. The desktop
// portal asks the user what to share and hands over a PipeWire stream, which
// gstreamer turns into a continuous MJPEG stream. The screenshot fallback
// writes a PNG per frame through the same portal, which crawls at ~2 fps.
func startScreenCast(o VideoOpts, cb func([]byte)) (func(), error) {
	fps := o.FPS
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return nil, errors.New("gst-launch-1.0 not found")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	portal := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	n := 0
	// request calls a portal method and waits for its Response signal, which
	// arrives only after the user has answered the portal's dialog.
	request := func(method string, args ...any) (map[string]dbus.Variant, error) {
		n++
		token := fmt.Sprintf("szept%d_%d", os.Getpid(), n)
		path := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + sender + "/" + token)
		if err := conn.AddMatchSignal(dbus.WithMatchObjectPath(path),
			dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response")); err != nil {
			return nil, err
		}
		ch := make(chan *dbus.Signal, 4)
		conn.Signal(ch)
		defer conn.RemoveSignal(ch)
		args[len(args)-1].(map[string]dbus.Variant)["handle_token"] = dbus.MakeVariant(token)
		if err := portal.Call("org.freedesktop.portal.ScreenCast."+method, 0, args...).Err; err != nil {
			return nil, err
		}
		timeout := time.After(3 * time.Minute)
		for {
			select {
			case s := <-ch:
				if s.Path != path || len(s.Body) < 2 {
					continue
				}
				if code, _ := s.Body[0].(uint32); code != 0 {
					return nil, errCancelled
				}
				res, _ := s.Body[1].(map[string]dbus.Variant)
				return res, nil
			case <-timeout:
				return nil, errors.New("portal nie odpowiedział")
			}
		}
	}
	fail := func(err error) (func(), error) { conn.Close(); return nil, err }

	res, err := request("CreateSession", map[string]dbus.Variant{"session_handle_token": dbus.MakeVariant(fmt.Sprintf("szept%d", os.Getpid()))})
	if err != nil {
		return fail(err)
	}
	h, _ := res["session_handle"].Value().(string)
	session := dbus.ObjectPath(h)
	// types 1|2 = a monitor or a window; cursor_mode 2 = drawn into the picture
	if _, err := request("SelectSources", session, map[string]dbus.Variant{
		"types": dbus.MakeVariant(uint32(3)), "cursor_mode": dbus.MakeVariant(uint32(2)),
	}); err != nil {
		return fail(err)
	}
	res, err = request("Start", session, "", map[string]dbus.Variant{})
	if err != nil {
		return fail(err)
	}
	streams, _ := res["streams"].Value().([][]any)
	if len(streams) == 0 || len(streams[0]) == 0 {
		return fail(errors.New("portal nie dał strumienia ekranu"))
	}
	node, _ := streams[0][0].(uint32)
	ow, oh := o.box()
	if len(streams[0]) > 1 {
		props, _ := streams[0][1].(map[string]dbus.Variant)
		if sz, ok := props["size"].Value().([]any); ok && len(sz) == 2 {
			sw, _ := sz[0].(int32)
			sh, _ := sz[1].(int32)
			ow, oh = fitSize(int(sw), int(sh), ow, oh)
		}
	}
	var fd dbus.UnixFD
	if err := portal.Call("org.freedesktop.portal.ScreenCast.OpenPipeWireRemote", 0, session,
		map[string]dbus.Variant{}).Store(&fd); err != nil {
		return fail(err)
	}
	pw := os.NewFile(uintptr(fd), "pipewire")

	// videorate drops the excess before the costly convert/scale/encode steps
	cmd := exec.Command("gst-launch-1.0", "-q",
		"pipewiresrc", "fd=3", fmt.Sprintf("path=%d", node), "do-timestamp=true", "!",
		"videorate", "drop-only=true", fmt.Sprintf("max-rate=%d", fps), "!",
		"videoconvert", "!", "videoscale", "!",
		// fixed caps: Hyprland's portal offers open size ranges, and videoscale
		// then fixates to 1x1 (a black tile) or fails with an overflow
		fmt.Sprintf("video/x-raw,width=%d,height=%d,pixel-aspect-ratio=1/1", ow, oh), "!",
		"jpegenc", fmt.Sprintf("quality=%d", o.Quality), "!", "fdsink", "fd=1")
	cmd.ExtraFiles = []*os.File{pw}
	out, err := cmd.StdoutPipe()
	if err != nil {
		pw.Close()
		return fail(err)
	}
	if err := cmd.Start(); err != nil {
		pw.Close()
		return fail(err)
	}
	pw.Close() // the child holds its own copy
	// videorate does not hold Hyprland's stream to fps (it delivered ~90/s),
	// so frames past the rate are dropped here as well
	gap, next := time.Second/time.Duration(fps), time.Time{}
	go splitJPEG(bufio.NewReaderSize(out, 1<<20), func(f []byte) {
		if now := time.Now(); now.After(next) {
			next = now.Add(gap)
			cb(f)
		}
	})
	return func() {
		cmd.Process.Kill()
		cmd.Wait()
		conn.Object("org.freedesktop.portal.Desktop", session).Call("org.freedesktop.portal.Session.Close", 0)
		conn.Close()
	}, nil
}
