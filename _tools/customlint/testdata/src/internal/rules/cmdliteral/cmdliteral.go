package cmdliteral

import "strings"

func example(nodeValue string) {
	// Bad: lowercase Dockerfile command literals.
	_ = strings.EqualFold(nodeValue, "from") // want `use command\.From constant instead of string literal "from" for Dockerfile instruction`
	_ = strings.EqualFold(nodeValue, "arg")  // want `use command\.Arg constant instead of string literal "arg" for Dockerfile instruction`
	_ = strings.EqualFold(nodeValue, "run")  // want `use command\.Run constant instead of string literal "run" for Dockerfile instruction`

	// Bad: uppercase Dockerfile command literals.
	_ = strings.EqualFold(nodeValue, "FROM")    // want `use command\.From constant instead of string literal "FROM" for Dockerfile instruction`
	_ = strings.EqualFold(nodeValue, "COPY")    // want `use command\.Copy constant instead of string literal "COPY" for Dockerfile instruction`
	_ = strings.EqualFold(nodeValue, "ADD")     // want `use command\.Add constant instead of string literal "ADD" for Dockerfile instruction`
	_ = strings.EqualFold(nodeValue, "ONBUILD") // want `use command\.Onbuild constant instead of string literal "ONBUILD" for Dockerfile instruction`

	// Bad: map key literals.
	m := map[string]bool{
		"entrypoint":  true, // want `use command\.Entrypoint constant instead of string literal "entrypoint" for Dockerfile instruction`
		"healthcheck": true, // want `use command\.Healthcheck constant instead of string literal "healthcheck" for Dockerfile instruction`
	}
	_ = m

	// Good: non-command strings — not flagged.
	_ = "hello world"
	_ = "FROM alpine:3.20"
	_ = "something"

	// Good: using a variable reference — not flagged.
	var commandFrom string
	_ = strings.EqualFold(nodeValue, commandFrom)
}
