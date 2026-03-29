package semantic

// ShellDirective is the subset of directive metadata needed by the semantic
// builder to seed stage shell state before the first instruction in a stage.
type ShellDirective struct {
	Line  int
	Shell string
}
