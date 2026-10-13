package agentrun

// ReadOnlyFileTools are the built-in tools a spec phase may be granted. Every
// one of them only reads; none runs a program. file_outline, find_symbol and
// find_references navigate by declaration.
var ReadOnlyFileTools = []string{
	"read_file", "list_files", "find_files", "search_files",
	"file_outline", "find_symbol", "find_references",
}

// ToolCodeSearch names the indexed search tool. It is granted only when the
// run has an index, so it is not part of ReadOnlyFileTools.
const ToolCodeSearch = "code_search"

// WithCodeSearch returns the grant with code_search appended when the run has
// an index, and the grant itself when it has none. The result is a copy, so
// the shared read-only list is never grown.
func WithCodeSearch(grant []string, on bool) []string {
	if !on {
		return grant
	}
	return append(append([]string(nil), grant...), ToolCodeSearch)
}
