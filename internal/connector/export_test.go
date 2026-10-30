package connector

// AllowPrivateHosts lets external tests (package connector_test) relay and
// mesh over loopback.
var AllowPrivateHosts = &allowPrivateMeshHosts
