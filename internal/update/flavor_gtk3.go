//go:build gtk3

package update

// GTK3 builds (webkit2gtk-4.1) keep the plain asset name: releases up to
// v0.4.0 shipped only this one, so clients from then still find it.
const flavor = ""
