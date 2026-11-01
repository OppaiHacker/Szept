package media

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/kbinani/screenshot"
	xdraw "golang.org/x/image/draw"
)

// VideoOpts: what a camera or screen share sends. Height is the target picture
// height (the width follows the source's aspect, within 16:9), Quality the
// JPEG quality 1..100. Zero fields take a default, see Or.
type VideoOpts struct {
	FPS     int `json:"fps,omitempty"`
	Height  int `json:"height,omitempty"`
	Quality int `json:"quality,omitempty"`
}

// Or fills unset fields from def and clamps the rest to sane bounds: a
// config value or a peer of the UI never asks for 500 fps or 8K.
func (o VideoOpts) Or(def VideoOpts) VideoOpts {
	pick := func(v, d, lo, hi int) int {
		if v <= 0 {
			v = d
		}
		return min(max(v, lo), hi)
	}
	return VideoOpts{
		FPS:     pick(o.FPS, def.FPS, 1, 60),
		Height:  pick(o.Height, def.Height, 144, 1440),
		Quality: pick(o.Quality, def.Quality, 10, 95),
	}
}

// box: the largest picture for this height, 16:9.
func (o VideoOpts) box() (int, int) { return o.Height * 16 / 9, o.Height }

// encodeJPEG scales down to fit maxW×maxH and encodes.
func encodeJPEG(img image.Image, maxW, maxH int, quality int) []byte {
	b := img.Bounds()
	if w, h := fitSize(b.Dx(), b.Dy(), maxW, maxH); w < b.Dx() {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		// Src, not Over: the frame is opaque, and Over blends every pixel
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		img = dst
	}
	var out bytes.Buffer
	jpeg.Encode(&out, img, &jpeg.Options{Quality: quality})
	return out.Bytes()
}

// fitSize scales w×h down to fit maxW×maxH, keeping the aspect ratio and even
// dimensions (encoders like them). Unknown sizes fall back to maxW×maxH.
func fitSize(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 {
		return maxW, maxH
	}
	if w > maxW {
		w, h = maxW, h*maxW/w
	}
	if h > maxH {
		w, h = w*maxH/h, maxH
	}
	return max(2, w&^1), max(2, h&^1)
}

// Displays lists the monitors StartScreen can capture, as "W×H", in
// display-index order. Empty on Wayland: the portal dialog picks there.
func Displays() []string {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return nil
	}
	out := []string{}
	for i := range screenshot.NumActiveDisplays() {
		b := screenshot.GetDisplayBounds(i)
		out = append(out, fmt.Sprintf("%d×%d", b.Dx(), b.Dy()))
	}
	return out
}

// StartScreen captures one display (index into Displays) as JPEG frames.
// X11/Windows capture directly; Wayland goes through the portal, which asks
// the user itself and ignores display.
func StartScreen(display int, o VideoOpts, cb func(jpegFrame []byte)) (stop func(), err error) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		stop, err := startScreenCast(o, cb)
		if err == nil || errors.Is(err, errCancelled) {
			return stop, err
		}
		log.Printf("screen: portal screencast: %v; falling back to screenshots", err)
	}
	if screenshot.NumActiveDisplays() == 0 {
		return nil, errors.New("no displays detected (Wayland without XWayland?)")
	}
	if display < 0 || display >= screenshot.NumActiveDisplays() {
		display = 0
	}
	if _, err := screenshot.CaptureDisplay(display); err != nil {
		return nil, fmt.Errorf("screen capture: %w", err)
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Second / time.Duration(o.FPS))
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				img, err := screenshot.CaptureDisplay(display)
				if err != nil {
					continue
				}
				w, h := o.box()
				cb(encodeJPEG(img, w, h, o.Quality))
			}
		}
	}()
	return func() { close(done) }, nil
}

// ---- ffmpeg camera fallback (works on Windows/dshow and Linux/v4l2) ----

// safeDevice keeps a configured camera name from turning into an ffmpeg flag.
// There is no shell here, so this is not command injection — but a value
// starting with "-" lands in argv as an option, and on Linux a camera is always
// a /dev node. An unusable value falls back to the platform default.
func safeDevice(device string) string {
	if strings.HasPrefix(device, "-") {
		return ""
	}
	if runtime.GOOS != "windows" && device != "" && !strings.HasPrefix(device, "/dev/") {
		return ""
	}
	return device
}

func ffmpegArgs(device string, o VideoOpts) []string {
	// never upscale; ffmpeg's mjpeg q:v runs 2 (best) .. 31
	out := []string{"-vf", fmt.Sprintf("scale=-2:'min(%d,ih)'", o.Height), "-r", fmt.Sprint(o.FPS),
		"-q:v", fmt.Sprint(max(2, (100-o.Quality)/5)), "-f", "mjpeg", "-"}
	device = safeDevice(device)
	switch runtime.GOOS {
	case "windows":
		if device == "" {
			device = "video=Integrated Camera"
		} else {
			device = "video=" + device
		}
		return append([]string{"-f", "dshow", "-i", device}, out...)
	default:
		if device == "" {
			device = "/dev/video0"
		}
		return append([]string{"-f", "v4l2", "-i", device}, out...)
	}
}

// hideConsole keeps a child process from opening its own console window
// (Windows: a GUI-subsystem app has no console to share). No-op elsewhere.
var hideConsole = func(*exec.Cmd) {}

// startFFmpegCamera shells out to ffmpeg and splits the MJPEG stream on
// SOI/EOI markers. Zero cgo, works wherever ffmpeg does.
func startFFmpegCamera(device string, o VideoOpts, cb func([]byte)) (func(), error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, errors.New("camera: missing v4l2 driver and ffmpeg not found in PATH")
	}
	cmd := exec.Command("ffmpeg", append([]string{"-loglevel", "quiet"}, ffmpegArgs(device, o)...)...)
	hideConsole(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { splitJPEG(bufio.NewReaderSize(out, 1<<20), cb) }()
	stopped := make(chan struct{})
	go func() { cmd.Wait(); close(stopped) }()
	return func() {
		cmd.Process.Kill()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}
	}, nil
}

var _ = io.Discard

// splitJPEG cuts an MJPEG stream into frames (SOI FF D8 .. EOI FF D9). It
// reads up to each 0xD9 in one call instead of a byte at a time.
func splitJPEG(r *bufio.Reader, cb func([]byte)) {
	soi := []byte{0xFF, 0xD8}
	var frame []byte
	for {
		chunk, err := r.ReadSlice(0xD9)
		frame = append(frame, chunk...)
		if err != nil && err != bufio.ErrBufferFull {
			return
		}
		if len(frame) > 4<<20 { // runaway, resync
			frame = frame[:0]
			continue
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		at := bytes.Index(frame, soi)
		if at < 0 {
			frame = frame[:0]
			continue
		}
		frame = frame[:copy(frame, frame[at:])]
		if len(frame) >= 4 && frame[len(frame)-2] == 0xFF { // EOI
			cb(bytes.Clone(frame))
			frame = frame[:0]
		}
	}
}
