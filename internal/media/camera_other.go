//go:build !linux

package media

// StartCamera on non-Linux: ffmpeg does the capture (dshow on Windows).
func StartCamera(device string, o VideoOpts, cb func(jpegFrame []byte)) (func(), error) {
	return startFFmpegCamera(device, o, cb)
}
