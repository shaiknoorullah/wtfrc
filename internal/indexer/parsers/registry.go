package parsers

var registry []Parser

func Register(p Parser) {
	registry = append(registry, p)
}

func ForFile(path string) Parser {
	for _, p := range registry {
		if p.CanParse(path) {
			return p
		}
	}
	return nil
}

func All() []Parser {
	return registry
}
