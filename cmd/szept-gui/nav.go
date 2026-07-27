package main

// Mobile navigation: stack of full-screen pages instead of dialogs.
//
// On phone a fyne dialog is a small square in the screen center — limited space,
// keyboard obscures it, and Back doesn't dismiss it. Therefore everything that is
// a window on desktop is a separate page with a title bar and back arrow on Android.
// u.page() branches: mobile -> page, desktop -> regular dialog.

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// appBar: top page bar — back arrow, title, and optional actions.
// Title clips to remaining space between arrow and actions;
// previously long titles pushed action buttons off screen.
func (u *ui) appBar(title string, back func(), actions ...fyne.CanvasObject) fyne.CanvasObject {
	t := newClipText(title, fs(17), cFg).bold()
	var left fyne.CanvasObject
	if back != nil {
		b := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), back)
		b.Importance = widget.LowImportance
		left = b
	} else {
		left = hspace(theme.Padding())
	}
	right := container.NewHBox(actions...)
	bg := canvas.NewRectangle(cDeep)
	row := container.NewBorder(nil, nil, left, right, vcenter(t))
	line := canvas.NewRectangle(cSep)
	line.SetMinSize(fyne.NewSize(1, 1))
	return container.NewStack(bg, container.NewVBox(row, line))
}

// backdrop: opaque page background that CONSUMES touch events. Plain
// rectangle background does not capture them — fyne looks for the top-most Tappable
// object, so tapping blank page area would hit bottom tab bar underneath.
func backdrop() fyne.CanvasObject {
	return tappable(canvas.NewRectangle(cBg), func() {}, nil)
}

// pushPage pushes page on top and slides it in from right edge. Stack renders
// layers on top of each other, so without backdrop underlying screen bled through.
func (u *ui) pushPage(title string, content fyne.CanvasObject, actions ...fyne.CanvasObject) {
	page := slidePage(container.NewStack(backdrop(), container.NewBorder(
		u.appBar(title, func() { u.popPage() }, actions...), nil, nil, nil, content)))
	u.root.Add(page)
	u.root.Refresh()
	slideIn(page, u.w.Canvas().Size().Width)
}

// popPage removes top page IMMEDIATELY — popAllPages loops while popping,
// and chatOpen() queries stack state right after return.
// Animated pop-out would leave page temporarily and cause state mismatch.
func (u *ui) popPage() bool {
	if u.root == nil || len(u.root.Objects) <= 1 {
		return false
	}
	u.root.Remove(u.root.Objects[len(u.root.Objects)-1])
	u.root.Refresh()
	return true
}

// popAllPages: upon entering another chat, returning to pages of previous
// chat (e.g. member list) is useless.
func (u *ui) popAllPages() {
	for u.popPage() {
	}
}

// page: same call works on both platforms — mobile gets full page,
// desktop gets dialog clipped to window size.
func (u *ui) page(title string, content fyne.CanvasObject, actions ...fyne.CanvasObject) {
	if u.mobile {
		u.pushPage(title, vscroll(container.NewPadded(content)), actions...)
		return
	}
	scr := container.NewVScroll(content)
	// height from content, not fixed: short page in 620px window was
	// an ocean of blank space with Close button stuck at the bottom
	scr.SetMinSize(u.dlgSize(440, content.MinSize().Height+theme.Padding()*2))
	dialog.ShowCustom(title, "Zamknij", scr, u.w)
}

// ---- dialogs ----
//
// dialog.ShowInformation/ShowError/ShowConfirm draw a huge semi-transparent
// icon ("?", "!") over half the dialog and use English button labels (lang.L("Yes")) —
// looking out of place in localized app. Custom* variants have neither default icon nor hardcoded text.

// dlgBody: dialog content. Wrapped text min width equals longest
// word, so without spacer dialog would shrink to 3-letter strip.
func (u *ui) dlgBody(text string) fyne.CanvasObject {
	t := widget.NewLabel(text)
	t.Wrapping = fyne.TextWrapWord
	return container.NewVBox(hspace(u.dlgSize(360, 0).Width), t)
}

func (u *ui) info(title, text string) {
	dialog.NewCustom(title, "OK", u.dlgBody(text), u.w).Show()
}

func (u *ui) fail(err error) {
	if err == nil {
		return
	}
	dialog.NewCustom("Nie wyszło", "OK", u.dlgBody(err.Error()), u.w).Show()
}

// confirm: yes/no question. Uses dialog even on mobile — single sentence
// and two buttons don't need full screen, and prompt is meant to pause action.
func (u *ui) confirm(text string, yes func()) {
	d := dialog.NewCustomConfirm("Na pewno?", "Tak", "Anuluj", u.dlgBody(text),
		func(ok bool) {
			if ok {
				yes()
			}
		}, u.w)
	d.SetConfirmImportance(widget.DangerImportance)
	d.Show()
}

// ---- page building blocks ----

// navRow: settings list row — icon, title, description, chevron.
func navRow(icon fyne.Resource, title, desc string, tap func()) fyne.CanvasObject {
	row := container.NewBorder(nil, nil,
		container.NewHBox(hspace(4), container.NewCenter(widget.NewIcon(icon)), hspace(10)),
		container.NewCenter(widget.NewIcon(theme.NavigateNextIcon())),
		vcenter(twoLine(title, desc, 14, 11)))
	return tappable(container.NewPadded(row), tap, nil)
}

// section: group header on settings pages.
func section(title string) fyne.CanvasObject {
	t := canvas.NewText(title, cMuted)
	t.TextSize = fs(11)
	t.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewPadded(t)
}

// bigButton: primary page action button.
func bigButton(label string, icon fyne.Resource, tap func()) *widget.Button {
	b := widget.NewButtonWithIcon(label, icon, tap)
	b.Importance = widget.HighImportance
	return b
}

// ---- tappable containers ----

// tapBox: arbitrary content responding to tap and long-press.
// Fyne lacks this control, without which list rows had to be buttons
// (single label, no avatar). Dragging finger doesn't trigger
// tap — mobile driver drops event when scroll starts.
type tapBox struct {
	widget.BaseWidget
	content fyne.CanvasObject
	tap     func()
	long    func()
	hover   *canvas.Rectangle
	anim    *fyne.Animation
}

func tappable(content fyne.CanvasObject, tap, long func()) *tapBox {
	t := &tapBox{content: content, tap: tap, long: long}
	t.hover = canvas.NewRectangle(color.Transparent)
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapBox) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(t.hover, t.content))
}

// Tapping during fling (scroll.go) means "stop", not "open" — standard
// Android list behavior. Without this, finger placed on moving list
// opened whatever chat it happened to touch.
func (t *tapBox) Tapped(*fyne.PointEvent) {
	if activeFling != nil {
		stopFling()
		return
	}
	if t.tap != nil {
		t.flash()
		t.tap()
	}
}

func (t *tapBox) TappedSecondary(*fyne.PointEvent) {
	if t.long != nil {
		t.flash()
		t.long()
	}
}

// pressTint: touch highlight overlay color.
var pressTint = color.NRGBA{0xff, 0xff, 0xff, 0x1c}

// flash: highlight flash upon release instead of pressed state.
//
// Highlight triggers on release because true pressed state
// required mobile.Touchable — which caused fyne driver to classify row as
// deepest target for drag events, breaking scroll STARTING on a row.
// Release flash is a small trade-off for working list scrolling.
func (t *tapBox) flash() {
	if t.anim != nil {
		t.anim.Stop()
	}
	t.hover.FillColor = pressTint
	t.hover.Refresh()
	t.anim = canvas.NewColorRGBAAnimation(pressTint, color.NRGBA{0xff, 0xff, 0xff, 0x00},
		220*msec, func(c color.Color) {
			t.hover.FillColor = c
			t.hover.Refresh()
		})
	t.anim.Start()
}

// ---- bottom navigation ----

// navTab: single tab in bottom bar. Custom because container.AppTabs
// at bottom provides narrow touch targets and no badge support.
type navTab struct {
	icon  fyne.Resource
	label string
	badge int
	img   *canvas.Image
	txt   *canvas.Text
	dot   *fyne.Container
	dotN  *canvas.Text
}

func (u *ui) buildBottomNav(labels []string, icons []fyne.Resource) fyne.CanvasObject {
	bg := canvas.NewRectangle(cDeep)
	row := container.NewGridWithColumns(len(labels))
	u.tabs = nil
	for i := range labels {
		i := i
		t := &navTab{icon: icons[i], label: labels[i]}
		t.img = canvas.NewImageFromResource(theme.NewThemedResource(icons[i]))
		t.img.FillMode = canvas.ImageFillContain
		t.img.SetMinSize(fyne.NewSize(22, 22))
		t.txt = canvas.NewText(labels[i], cMuted)
		t.txt.TextSize = fs(10)
		t.txt.Alignment = fyne.TextAlignCenter
		t.dotN = canvas.NewText("", color.NRGBA{0xff, 0xff, 0xff, 0xff})
		t.dotN.TextSize = fs(10)
		t.dotN.TextStyle = fyne.TextStyle{Bold: true}
		badgeBg := canvas.NewRectangle(cRed)
		badgeBg.CornerRadius = 8
		t.dot = container.NewGridWrap(fyne.NewSize(18, 16),
			container.NewStack(badgeBg, container.NewCenter(t.dotN)))
		t.dot.Hide()
		icon := container.NewStack(container.NewCenter(container.NewGridWrap(
			fyne.NewSize(22, 22), t.img)),
			container.NewHBox(layout.NewSpacer(), container.NewVBox(t.dot, layout.NewSpacer())))
		cell := container.NewPadded(container.NewVBox(icon, t.txt))
		row.Add(tappable(cell, func() { u.selectTab(i) }, nil))
		u.tabs = append(u.tabs, t)
	}
	line := canvas.NewRectangle(cSep)
	line.SetMinSize(fyne.NewSize(1, 1))
	return container.NewStack(bg, container.NewVBox(line, row))
}

// selectTab switches main tab (bottom bar) and pops stacked pages.
func (u *ui) selectTab(i int) {
	if i < 0 || i >= len(u.tabViews) {
		return
	}
	u.popAllPages()
	prev := u.tab
	u.tab = i
	u.tabBody.Objects = []fyne.CanvasObject{u.tabViews[i]}
	u.tabBody.Refresh()
	// new tab slides in from the side it "came from" — 24px is enough
	// for transition without annoying delay
	if prev != i {
		if prev < i {
			slideIn(u.tabBody, 24)
		} else {
			slideIn(u.tabBody, -24)
		}
	}
	for j, t := range u.tabs {
		if j == i {
			t.txt.Color = cAccent
			t.img.Resource = theme.NewPrimaryThemedResource(t.icon)
		} else {
			t.txt.Color = cMuted
			t.img.Resource = theme.NewThemedResource(t.icon)
		}
		t.txt.Refresh()
		t.img.Refresh()
	}
	u.checkClipboard()
	u.refresh()
}

// setTabBadge: unread count badge on tab icon.
func (u *ui) setTabBadge(i, n int) {
	if i < 0 || i >= len(u.tabs) {
		return
	}
	t := u.tabs[i]
	if n <= 0 {
		t.dot.Hide()
		return
	}
	s := "9+"
	if n < 10 {
		s = string(rune('0' + n))
	}
	t.dotN.Text = s
	t.dotN.Refresh()
	t.dot.Show()
}
