//go:build !unix

package specrun

// processAlive cannot tell without signals; a recorded process is taken to
// have ended.
func processAlive(int) bool { return false }
