package parsers

// EntryType classifies the kind of config entry.
type EntryType string

const (
	EntryKeybind  EntryType = "keybind"
	EntryAlias    EntryType = "alias"
	EntryFunction EntryType = "function"
	EntryExport   EntryType = "export"
	EntrySetting  EntryType = "setting"
	EntryService  EntryType = "service"
	EntryHost     EntryType = "host"
)

// RawEntry is the output of a parser before LLM enrichment.
type RawEntry struct {
	Tool         string
	Type         EntryType
	RawBinding   string
	RawAction    string
	SourceFile   string
	SourceLine   int
	ContextLines string
}

// Parser reads a config file and extracts raw entries.
type Parser interface {
	Name() string
	CanParse(path string) bool
	Parse(path string) ([]RawEntry, error)
}
