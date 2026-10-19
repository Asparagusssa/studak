package models

const (
	SearchModeMyOnly   = "my_only"
	SearchModeSelected = "selected"
	SearchModeAll      = "all"
)

func IsValidSearchMode(mode string) bool {
	switch mode {
	case SearchModeMyOnly, SearchModeSelected, SearchModeAll:
		return true
	}
	return false
}
