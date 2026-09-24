// Package buildinfo holds the app version as a Go constant.
//
// gd build cannot inject ldflags, so the version is spelled here and a test
// keeps it in step with graphics/export_presets.cfg (the release source of
// truth). Both the app's MCP server and the stdio bridge report it, which lets
// the bridge warn when it is talking to a Bufflehead of a different version.
package buildinfo

// Version is the semantic version of this build, e.g. "0.29.0".
const Version = "0.30.0"
