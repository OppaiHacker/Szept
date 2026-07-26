//go:build linux

package media

import (
	"image"
	"time"

	"github.com/blackjack/webcam"
)

const (
	fmtMJPG = webcam.PixelFormat(0x47504A4D) // 'MJPG'
	fmtYUYV = webcam.PixelFormat(0x56595559) // 'YUYV'
)

// StartCamera: native V4L2 first (pure Go), ffmpeg as fallback.
func StartCamera(device string, fps int, cb func(jpegFrame []byte)) (func(), error) {
	if device == "" {
		device = "/dev/video0"
	}
	cam, err := webcam.Open(device)
	if err != nil {
		return startFFmpegCamera(device, cb)
	}
	formats := cam.GetSupportedFormats()
	var pick webcam.PixelFormat
	for _, f := range []webcam.PixelFormat{fmtMJPG, fmtYUYV} {
		if _, ok := formats[f]; ok {
			pick = f
			break
		}
	}
	if pick == 0 {
		cam.Close()
		return startFFmpegCamera(device, cb)
	}
	pf, w, h, err := cam.SetImageFormat(pick, 640, 480)
	if err != nil {
		cam.Close()
		return startFFmpegCamera(device, cb)
	}
	if err := cam.StartStreaming(); err != nil {
		cam.Close()
		return startFFmpegCamera(device, cb)
	}
	done := make(chan struct{})
	go func() {
		defer cam.Close()
		interval := time.Second / time.Duration(fps)
		last := time.Time{}
		for {
			select {
			case <-done:
				return
			default:
			}
			if err := cam.WaitForFrame(1); err != nil {
				if _, ok := err.(*webcam.Timeout); ok {
					continue
				}
				return
			}
			frame, err := cam.ReadFrame()
			if err != nil || len(frame) == 0 {
				continue
			}
			if time.Since(last) < interval {
				continue // rate limit
			}
			last = time.Now()
			switch pf {
			case fmtMJPG:
				cp := make([]byte, len(frame))
				copy(cp, frame)
				cb(cp)
			case fmtYUYV:
				cb(encodeJPEG(yuyvToImage(frame, int(w), int(h)), 640, 60))
			}
		}
	}()
	return func() { close(done) }, nil
}

func yuyvToImage(fr []byte, w, h int) image.Image {
	img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio422)
	for i := 0; i+3 < len(fr) && i/2 < len(img.Y)-1; i += 4 {
		yi := i / 2
		ci := i / 4
		img.Y[yi] = fr[i]
		img.Y[yi+1] = fr[i+2]
		if ci < len(img.Cb) {
			img.Cb[ci] = fr[i+1]
			img.Cr[ci] = fr[i+3]
		}
	}
	return img
}
