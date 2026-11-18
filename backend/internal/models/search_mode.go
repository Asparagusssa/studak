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

const (
	SearchGenderAll    = "all"
	SearchGenderFemale = "female"
	SearchGenderMale   = "male"
)

func IsValidSearchGender(gender string) bool {
	switch gender {
	case SearchGenderAll, SearchGenderFemale, SearchGenderMale:
		return true
	}
	return false
}
