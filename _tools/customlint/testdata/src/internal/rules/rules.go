package rules

// GoodRule is a properly documented rule.
type GoodRule struct {
	MaxValue int
}

type BadRule struct { // want "exported rule struct BadRule should have a documentation comment"
	Value int
}
