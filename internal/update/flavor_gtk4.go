//go:build !gtk3

package update

// GTK4 builds (webkitgtk-6.0) get their own asset: the GTK3 one does not
// load on a system with only webkitgtk-6.0, and the other way round.
const flavor = "-gtk4"
