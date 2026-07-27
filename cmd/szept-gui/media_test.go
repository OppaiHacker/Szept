package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/test"
)

// showFileDlg must show window before resize — reverse order
// crashes app (fyne 2.8: FileDialog.MinSize accesses window from Show()).
func TestShowFileDlgDoesNotPanic(t *testing.T) {
	w := test.NewWindow(nil)
	defer w.Close()
	u := &ui{w: w}

	u.showFileDlg(dialog.NewFileOpen(func(fyne.URIReadCloser, error) {}, w))

	save := dialog.NewFileSave(func(fyne.URIWriteCloser, error) {}, w)
	save.SetFileName("a.png")
	u.showFileDlg(save)

	u.mobile = true // on phone Resize is skipped (native picker)
	u.showFileDlg(dialog.NewFileOpen(func(fyne.URIReadCloser, error) {}, w))
}
