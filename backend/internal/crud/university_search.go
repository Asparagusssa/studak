package crud

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"studak/internal/models"
)

const (
	universitySearchLimit    = 10
	MaxUniversitySearchRunes = 100
)

// SearchUniversities ranks exact names and abbreviations before typo-tolerant
// suggestions. Scoring runs over the small reference catalog, not user data.
func SearchUniversities(db *gorm.DB, query string) ([]models.University, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > MaxUniversitySearchRunes {
		return nil, invalid("university search query is too long")
	}
	universities := []models.University{}
	q := db.Order("name")
	if query == "" {
		q = q.Limit(universitySearchLimit)
	}
	if err := q.Find(&universities).Error; err != nil {
		return nil, err
	}
	if query == "" {
		return universities, nil
	}
	query = normalizeUniversityName(query)
	if query == "" {
		return []models.University{}, nil
	}
	type match struct {
		university models.University
		score      int
	}
	matches := make([]match, 0, len(universities))
	for _, university := range universities {
		if score, ok := universityScore(university, query); ok {
			matches = append(matches, match{university, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score < matches[j].score })
	result := make([]models.University, 0, min(len(matches), universitySearchLimit))
	for _, match := range matches[:min(len(matches), universitySearchLimit)] {
		result = append(result, match.university)
	}
	return result, nil
}

func normalizeUniversityName(value string) string {
	value = strings.ReplaceAll(strings.ToLower(value), "ё", "е")
	return strings.Join(strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

func universityScore(university models.University, query string) (int, bool) {
	short, full := normalizeUniversityName(university.ShortName), normalizeUniversityName(university.Name)
	compact := strings.ReplaceAll(query, " ", "")
	if query == short || query == full {
		return 0, true
	}
	for index, word := range strings.Fields(short) {
		if word == compact {
			if index == 0 {
				return 0, true
			}
			return 1, true
		}
	}
	if strings.HasPrefix(short, query) || strings.HasPrefix(full, query) {
		return 10, true
	}
	if strings.Contains(short, query) || strings.Contains(full, query) {
		return 20, true
	}
	words := strings.Fields(short + " " + full)
	score := 100
	for _, part := range strings.Fields(query) {
		input := []rune(part)
		allowed := 0
		if len(input) >= 3 {
			allowed = 1
		}
		if len(input) >= 5 {
			allowed = 2
		}
		best := 1000
		for _, word := range words {
			if strings.HasPrefix(word, part) {
				best = 0
				break
			}
			candidate := []rune(word)
			if abs(len(input)-len(candidate)) > allowed {
				continue
			}
			if distance := universityEditDistance(input, candidate); distance <= allowed {
				best = min(best, distance*10+abs(len(input)-len(candidate)))
			}
		}
		if best == 1000 {
			return 0, false
		}
		score += best
	}
	return score, true
}

// universityEditDistance counts adjacent swapped letters as a single typo.
func universityEditDistance(input, candidate []rune) int {
	previous := make([]int, len(candidate)+1)
	for i := range previous {
		previous[i] = i
	}
	var beforePrevious []int
	for i, letter := range input {
		current := make([]int, len(candidate)+1)
		current[0] = i + 1
		for j, other := range candidate {
			cost := 0
			if letter != other {
				cost = 1
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
			if i > 0 && j > 0 && letter == candidate[j-1] && input[i-1] == other {
				current[j+1] = min(current[j+1], beforePrevious[j-1]+1)
			}
		}
		beforePrevious, previous = previous, current
	}
	return previous[len(candidate)]
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
