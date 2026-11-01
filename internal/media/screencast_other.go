//go:build !linux

package media

import "errors"

var errCancelled = errors.New("udostępnianie ekranu anulowane")

func startScreenCast(int, func([]byte)) (func(), error) {
	return nil, errors.New("portal screencast is Linux only")
}
