//go:build !linux

package media

// StartCamera on non-Linux: ffmpeg does the capture (dshow on Windows).
func StartCamera(device string, fps int, cb func(jpegFrame []byte)) (func(), error) {
	_ = fps // ffmpeg paces itself; scale/quality set in args
	return startFFmpegCamera(device, cb)
}
