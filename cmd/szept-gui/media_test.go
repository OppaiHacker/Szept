package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/test"
)

// showFileDlg musi pokazać okno przed zmianą rozmiaru — odwrotna kolejność
// wywala aplikację (fyne 2.8: FileDialog.MinSize sięga do okna z Show()).
func TestShowFileDlgDoesNotPanic(t *testing.T) {
	w := test.NewWindow(nil)
	defer w.Close()
	u := &ui{w: w}

	u.showFileDlg(dialog.NewFileOpen(func(fyne.URIReadCloser, error) {}, w))

	save := dialog.NewFileSave(func(fyne.URIWriteCloser, error) {}, w)
	save.SetFileName("a.png")
	u.showFileDlg(save)

	u.mobile = true // na telefonie Resize jest pomijany (natywny picker)
	u.showFileDlg(dialog.NewFileOpen(func(fyne.URIReadCloser, error) {}, w))
}
