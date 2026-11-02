// Package opus wraps libopus 1.5.2 (vendored C, BSD, see libopus/COPYING):
// 48 kHz mono voice, 20 ms packets. Generic C build, no SIMD or DNN parts;
// each z_*.c pulls in one libopus source because cgo builds this dir only.
package opus

/*
#cgo CFLAGS: -O2 -DOPUS_BUILD -DUSE_ALLOCA -DHAVE_LRINTF -I${SRCDIR}/libopus/include -I${SRCDIR}/libopus/celt -I${SRCDIR}/libopus/silk -I${SRCDIR}/libopus/silk/float -Wno-stringop-overflow
#cgo LDFLAGS: -lm
#include "opus.h"

// opus_*_ctl are variadic, which cgo cannot call
static int enc_setup(OpusEncoder *e, int bitrate) {
	opus_encoder_ctl(e, OPUS_SET_BITRATE(bitrate));
	opus_encoder_ctl(e, OPUS_SET_COMPLEXITY(10));
	opus_encoder_ctl(e, OPUS_SET_LSB_DEPTH(24));
	opus_encoder_ctl(e, OPUS_SET_SIGNAL(bitrate >= 128000 ? OPUS_SIGNAL_MUSIC : (bitrate > 32000 ? OPUS_AUTO : OPUS_SIGNAL_VOICE)));
	opus_encoder_ctl(e, OPUS_SET_INBAND_FEC(1));
	return opus_encoder_ctl(e, OPUS_SET_PACKET_LOSS_PERC(2));
}
static void enc_bitrate(OpusEncoder *e, int bitrate) {
	opus_encoder_ctl(e, OPUS_SET_BITRATE(bitrate));
	opus_encoder_ctl(e, OPUS_SET_SIGNAL(bitrate >= 128000 ? OPUS_SIGNAL_MUSIC : (bitrate > 32000 ? OPUS_AUTO : OPUS_SIGNAL_VOICE)));
}
static void enc_loss(OpusEncoder *e, int perc) {
	opus_encoder_ctl(e, OPUS_SET_PACKET_LOSS_PERC(perc));
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

const (
	Rate    = 48000
	Frame   = 960   // samples per 20 ms packet
	Bitrate = 48000 // 48k mono gives transparent fullband speech/audio without choking FEC
)

// Encoder and Decoder keep libopus state in Go memory (opus_*_init on a
// byte slice), so there is nothing to free. The state holds no pointers.
type Encoder struct {
	st      []byte
	bitrate int
	loss    int
}

func NewEncoder() (*Encoder, error) {
	e := &Encoder{st: make([]byte, C.opus_encoder_get_size(1)), bitrate: Bitrate, loss: 2}
	if C.opus_encoder_init(e.ptr(), Rate, 1, C.OPUS_APPLICATION_VOIP) != C.OPUS_OK ||
		C.enc_setup(e.ptr(), Bitrate) != C.OPUS_OK {
		return nil, errors.New("opus: encoder init")
	}
	return e, nil
}

// SetBitrate in bit/s (0 = Bitrate); cheap when unchanged, so callers may
// pass the current setting with every frame.
func (e *Encoder) SetBitrate(b int) {
	if b == 0 {
		b = Bitrate
	}
	if b != e.bitrate {
		e.bitrate = b
		C.enc_bitrate(e.ptr(), C.int(b))
	}
}

// SetPacketLoss updates expected packet loss percentage (0..25) for inband FEC.
func (e *Encoder) SetPacketLoss(perc int) {
	if perc < 0 {
		perc = 0
	} else if perc > 25 {
		perc = 25
	}
	if perc != e.loss {
		e.loss = perc
		C.enc_loss(e.ptr(), C.int(perc))
	}
}

func (e *Encoder) ptr() *C.OpusEncoder { return (*C.OpusEncoder)(unsafe.Pointer(&e.st[0])) }

// Encode one 20 ms frame of s16le mono PCM (Frame samples).
func (e *Encoder) Encode(pcm []byte) ([]byte, error) {
	if len(pcm) != Frame*2 {
		return nil, errors.New("opus: frame size")
	}
	out := make([]byte, 1275) // max packet
	n := C.opus_encode(e.ptr(), (*C.opus_int16)(unsafe.Pointer(&pcm[0])), Frame,
		(*C.uchar)(unsafe.Pointer(&out[0])), C.opus_int32(len(out)))
	if n < 0 {
		return nil, errors.New("opus: encode")
	}
	return out[:n], nil
}

// EncodeFloat encodes one 20 ms frame of normalized [-1.0, 1.0] float32 mono PCM.
func (e *Encoder) EncodeFloat(pcm []float32) ([]byte, error) {
	if len(pcm) != Frame {
		return nil, errors.New("opus: frame size")
	}
	out := make([]byte, 1275)
	n := C.opus_encode_float(e.ptr(), (*C.float)(unsafe.Pointer(&pcm[0])), Frame,
		(*C.uchar)(unsafe.Pointer(&out[0])), C.opus_int32(len(out)))
	if n < 0 {
		return nil, errors.New("opus: encode_float")
	}
	return out[:n], nil
}

type Decoder struct{ st []byte }

func NewDecoder() *Decoder {
	d := &Decoder{make([]byte, C.opus_decoder_get_size(1))}
	C.opus_decoder_init(d.ptr(), Rate, 1) // only fails on bad rate/channels
	return d
}

func (d *Decoder) ptr() *C.OpusDecoder { return (*C.OpusDecoder)(unsafe.Pointer(&d.st[0])) }

// Decode one packet to s16le PCM. pkt nil = packet lost: libopus fills in a
// guess (PLC). fec: rebuild the packet *before* pkt from pkt's FEC data.
func (d *Decoder) Decode(pkt []byte, fec bool) ([]byte, error) {
	out := make([]byte, Frame*2)
	var p *C.uchar
	if len(pkt) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&pkt[0]))
	}
	f := C.int(0)
	if fec {
		f = 1
	}
	n := C.opus_decode(d.ptr(), p, C.opus_int32(len(pkt)),
		(*C.opus_int16)(unsafe.Pointer(&out[0])), Frame, f)
	if n < 0 {
		return nil, errors.New("opus: decode")
	}
	return out[:2*n], nil
}

// DecodeFloat decodes one packet to normalized float32 mono PCM.
func (d *Decoder) DecodeFloat(pkt []byte, fec bool) ([]float32, error) {
	out := make([]float32, Frame)
	var p *C.uchar
	if len(pkt) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&pkt[0]))
	}
	f := C.int(0)
	if fec {
		f = 1
	}
	n := C.opus_decode_float(d.ptr(), p, C.opus_int32(len(pkt)),
		(*C.float)(unsafe.Pointer(&out[0])), Frame, f)
	if n < 0 {
		return nil, errors.New("opus: decode_float")
	}
	return out[:n], nil
}
