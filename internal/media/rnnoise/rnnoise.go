// Package rnnoise wraps xiph/rnnoise v0.1.1 (vendored C, BSD, see COPYING):
// a small neural net that strips background noise (fans, keyboards, street)
// from 48 kHz mono speech, 10 ms at a time.
package rnnoise

/*
#cgo CFLAGS: -O2 -Wno-unused-function
#cgo LDFLAGS: -lm
#include <stdlib.h>
#include "rnnoise.h"
*/
import "C"

import "unsafe"

// FrameSize: samples per Process call (10 ms @ 48 kHz).
const FrameSize = 480

type State struct{ st *C.DenoiseState }

func New() *State { return &State{C.rnnoise_create(nil)} }

// Process denoises one frame in place. Samples are int16-scale floats, as
// rnnoise expects. Returns the probability (0..1) that the frame is speech.
func (s *State) Process(frame *[FrameSize]float32) float32 {
	p := (*C.float)(unsafe.Pointer(&frame[0]))
	return float32(C.rnnoise_process_frame(s.st, p, p))
}

func (s *State) Close() {
	if s.st != nil {
		C.rnnoise_destroy(s.st)
		s.st = nil
	}
}
