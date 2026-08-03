package media

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/kbinani/screenshot"
	xdraw "golang.org/x/image/draw"
)

// encodeJPEG scales to maxW and encodes. Quality tuned for data-channel frames.
func encodeJPEG(img image.Image, maxW int, quality int) []byte {
	b := img.Bounds()
	if b.Dx() > maxW {
		h := b.Dy() * maxW / b.Dx()
		dst := image.NewRGBA(image.Rect(0, 0, maxW, h))
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
		img = dst
	}
	var out bytes.Buffer
	jpeg.Encode(&out, img, &jpeg.Options{Quality: quality})
	return out.Bytes()
}

// StartScreen captures the primary display as JPEG frames.
// X11/Windows OK; Wayland needs XWayland (portal capture is the upgrade path).
func StartScreen(fps int, cb func(jpegFrame []byte)) (stop func(), err error) {
	if screenshot.NumActiveDisplays() == 0 {
		return nil, errors.New("no displays detected (Wayland without XWayland?)")
	}
	if _, err := screenshot.CaptureDisplay(0); err != nil {
		return nil, fmt.Errorf("screen capture: %w", err)
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Second / time.Duration(fps))
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				img, err := screenshot.CaptureDisplay(0)
				if err != nil {
					continue
				}
				cb(encodeJPEG(img, 1280, 50))
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

func ffmpegArgs(device string) []string {
	device = safeDevice(device)
	switch runtime.GOOS {
	case "windows":
		if device == "" {
			device = "video=Integrated Camera"
		} else {
			device = "video=" + device
		}
		return []string{"-f", "dshow", "-i", device, "-vf", "scale=640:-1", "-q:v", "8", "-f", "mjpeg", "-"}
	default:
		if device == "" {
			device = "/dev/video0"
		}
		return []string{"-f", "v4l2", "-i", device, "-vf", "scale=640:-1", "-q:v", "8", "-f", "mjpeg", "-"}
	}
}

// startFFmpegCamera shells out to ffmpeg and splits the MJPEG stream on
// SOI/EOI markers. Zero cgo, works wherever ffmpeg does.
func startFFmpegCamera(device string, cb func([]byte)) (func(), error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, errors.New("camera: missing v4l2 driver and ffmpeg not found in PATH")
	}
	cmd := exec.Command("ffmpeg", append([]string{"-loglevel", "quiet"}, ffmpegArgs(device)...)...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		r := bufio.NewReaderSize(out, 1<<20)
		var frame []byte
		in := false
		for {
			b, err := r.ReadByte()
			if err != nil {
				return
			}
			if !in {
				if b == 0xFF {
					nb, err := r.ReadByte()
					if err != nil {
						return
					}
					if nb == 0xD8 { // SOI
						frame = append(frame[:0], 0xFF, 0xD8)
						in = true
					}
				}
				continue
			}
			frame = append(frame, b)
			if b == 0xD9 && len(frame) > 2 && frame[len(frame)-2] == 0xFF { // EOI
				cp := make([]byte, len(frame))
				copy(cp, frame)
				cb(cp)
				in = false
			}
			if len(frame) > 4<<20 { // runaway, resync
				in = false
			}
		}
	}()
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
