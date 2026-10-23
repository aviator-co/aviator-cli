//go:build !experimental

package config

// Experimental reports whether this binary was built with the experimental
// tag. No release sets it; only local `just build-experimental` builds do.
const Experimental = false
