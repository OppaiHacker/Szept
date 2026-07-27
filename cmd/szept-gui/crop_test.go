package main

import (
	"image"
	"testing"

	"fyne.io/fyne/v2"
)

// The frame is kept inside the source no matter how far the finger drags, and
// it always holds the target proportions — the rectangle it returns is what
// gets cut out of the original bytes.
func TestCropRegion(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	c := newCropBox(src, fyne.NewSize(120, 120), 1, 1, false)

	if got := c.region(); got != image.Rect(100, 0, 300, 200) {
		t.Fatalf("kadr startowy %v", got)
	}
	c.setZoom(2)
	if got := c.region(); got != image.Rect(150, 50, 250, 150) {
		t.Fatalf("kadr po powiększeniu %v", got)
	}
	c.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(1000, 1000)})
	if got := c.region(); got != image.Rect(0, 0, 100, 100) {
		t.Fatalf("kadr po zjechaniu za krawędź %v", got)
	}
	// zoom cannot go below "whole image", so the banner strip stays full width
	b := newCropBox(src, fyne.NewSize(120, 40), 3, 1, false)
	b.setZoom(0.1)
	if got := b.region(); got != image.Rect(0, 33, 400, 166) {
		t.Fatalf("pasek %v", got)
	}
}
