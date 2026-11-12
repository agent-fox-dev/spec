//go:build !unix

package specrun

// isSpecrun cannot tell without signals; a recorded process is taken to have
// ended.
func isSpecrun(int) bool { return false }
