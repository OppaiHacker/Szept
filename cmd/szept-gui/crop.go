package main

// Framing an avatar or a banner: a popup where the picked image can be moved
// and zoomed before it is cut to shape. What the box shows IS the result —
// there is no separate "before" view to compare against, so the frame never
// lies about what lands on the profile.

import (
	"image"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	xdraw "golang.org/x/image/draw"

	"szept/internal/attach"
)

// maxZoom: how far into the source the frame may close in. Past this the
// output is mostly interpolation, and the user only sees blur.
const maxZoom = 5

// cropBox: the framing surface. Drag pans, the slider (or the wheel) zooms.
// It keeps the frame in source pixels, so the rectangle handed back at the end
// is independent of how large the popup happened to be drawn.
type cropBox struct {
	widget.BaseWidget
	src    image.Image
	view   fyne.Size
	aw, ah int  // target proportions
	round  bool // avatar: preview through the same circle the app renders
	zoom   float64
	cx, cy float64 // frame center, in source pixels
	img    *canvas.Image
	onZoom func(float64) // keeps the slider in step with wheel zooming
}

func newCropBox(src image.Image, view fyne.Size, aw, ah int, round bool) *cropBox {
	b := src.Bounds()
	c := &cropBox{
		src: src, view: view, aw: aw, ah: ah, round: round, zoom: 1,
		cx: float64(b.Min.X) + float64(b.Dx())/2,
		cy: float64(b.Min.Y) + float64(b.Dy())/2,
	}
	c.img = canvas.NewImageFromImage(src)
	c.img.FillMode = canvas.ImageFillContain
	c.img.SetMinSize(view)
	c.ExtendBaseWidget(c)
	c.redraw()
	return c
}

func (c *cropBox) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewGridWrap(c.view, c.img))
}

// region: the framed rectangle in source pixels. Clamped here rather than at
// every caller — pan, zoom and the initial center all funnel through it.
func (c *cropBox) region() image.Rectangle {
	b := c.src.Bounds()
	sw, sh := float64(b.Dx()), float64(b.Dy())
	fw, fh := sw, sw*float64(c.ah)/float64(c.aw)
	if fh > sh { // taller than the source — the height is what limits us
		fw, fh = sh*float64(c.aw)/float64(c.ah), sh
	}
	fw, fh = fw/c.zoom, fh/c.zoom
	c.cx = clampF(c.cx, float64(b.Min.X)+fw/2, float64(b.Max.X)-fw/2)
	c.cy = clampF(c.cy, float64(b.Min.Y)+fh/2, float64(b.Max.Y)-fh/2)
	x, y := int(c.cx-fw/2+0.5), int(c.cy-fh/2+0.5)
	return image.Rect(x, y, x+int(fw+0.5), y+int(fh+0.5)).Intersect(b)
}

func (c *cropBox) redraw() {
	r := c.region()
	dst := image.NewRGBA(image.Rect(0, 0, int(c.view.Width), int(c.view.Height)))
	// ApproxBiLinear, not CatmullRom: this runs on every drag event, and the
	// bytes that count are re-sampled properly by attach on the way out
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), c.src, r, xdraw.Over, nil)
	var out image.Image = dst
	if c.round {
		out = circleMask(dst)
	}
	c.img.Image = out
	c.img.Refresh()
}

func (c *cropBox) setZoom(z float64) {
	c.zoom = clampF(z, 1, maxZoom)
	c.redraw()
}

// Dragged: the finger pushes the image, so the frame travels the other way.
func (c *cropBox) Dragged(e *fyne.DragEvent) {
	r := c.region()
	c.cx -= float64(e.Dragged.DX) * float64(r.Dx()) / float64(c.view.Width)
	c.cy -= float64(e.Dragged.DY) * float64(r.Dy()) / float64(c.view.Height)
	c.redraw()
}

func (c *cropBox) DragEnd() {}

// Scrolled: wheel zoom on desktop. The slider is told about it so both
// controls always show the same number.
func (c *cropBox) Scrolled(e *fyne.ScrollEvent) {
	z := clampF(c.zoom*(1+float64(e.Scrolled.DY)/300), 1, maxZoom)
	if c.onZoom != nil {
		c.onZoom(z) // slider callback loops back into setZoom
		return
	}
	c.setZoom(z)
}

func clampF(v, lo, hi float64) float64 {
	if hi < lo { // frame wider than the source after rounding — center it
		return (lo + hi) / 2
	}
	return max(lo, min(v, hi))
}

// cropDlg: popup between picking a file and saving it. done receives the
// framed region in source pixels; attach cuts the original bytes with it, so
// nothing is re-encoded twice and full quality survives.
func (u *ui) cropDlg(title string, data []byte, aw, ah int, round bool, done func(image.Rectangle)) {
	src, err := attach.SourceImage(data)
	if err != nil {
		u.fail(err)
		return
	}
	w := float32(300)
	if cw := u.w.Canvas().Size().Width; cw > 160 {
		w = fyne.Min(w, cw-90)
	}
	view := fyne.NewSize(w, w*float32(ah)/float32(aw))
	box := newCropBox(src, view, aw, ah, round)

	zoom := widget.NewSlider(1, maxZoom)
	zoom.Step = 0.01
	zoom.Value = 1
	zoom.OnChanged = func(v float64) { box.setZoom(v) }
	box.onZoom = zoom.SetValue

	body := container.NewVBox(
		container.NewCenter(box),
		container.NewBorder(nil, nil,
			widget.NewIcon(theme.ZoomOutIcon()), widget.NewIcon(theme.ZoomInIcon()), zoom),
		mutedWrap("Przeciągnij obrazek, żeby wybrać kadr, i suwakiem ustaw powiększenie. "+
			"Ramka pokazuje dokładnie to, co zobaczą inni."),
	)
	d := dialog.NewCustomConfirm(title, "Ustaw", "Anuluj", body, func(ok bool) {
		if ok {
			done(box.region())
		}
	}, u.w)
	d.Show()
}
