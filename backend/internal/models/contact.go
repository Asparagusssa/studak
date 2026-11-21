package models

import (
	"net/url"
	"strings"
	"unicode"
)

// NormalizeMaxContact accepts a phone number or a public messenger contact.
// It returns the canonical value kept private until a mutual match.
func NormalizeMaxContact(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 {
		return "", false
	}
	if strings.HasPrefix(value, "@") {
		username := strings.TrimPrefix(value, "@")
		if username != "" && IsValidTelegramUsername(username) {
			return "https://t.me/" + username, true
		}
		return "", false
	}
	if strings.HasPrefix(value, "t.me/") || strings.HasPrefix(value, "max.ru/") {
		value = "https://" + value
	}
	if strings.HasPrefix(value, "https://") {
		if IsValidMaxProfileURL(value) {
			return value, true
		}
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host != "t.me" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", false
		}
		username := strings.TrimPrefix(u.Path, "/")
		if username == "" || !IsValidTelegramUsername(username) {
			return "", false
		}
		switch strings.ToLower(username) {
		case "share", "joinchat", "login", "proxy", "iv", "addstickers", "c", "s":
			return "", false
		}
		return "https://t.me/" + username, true
	}
	var digits strings.Builder
	for i, letter := range value {
		switch {
		case letter >= '0' && letter <= '9':
			digits.WriteRune(letter)
		case letter == '+' && i == 0:
		case unicode.IsSpace(letter) || letter == '(' || letter == ')' || letter == '-' || letter == '‑' || letter == '–':
		default:
			return "", false
		}
	}
	number := digits.String()
	if len(number) == 11 && number[0] == '8' && !strings.HasPrefix(value, "+") {
		number = "7" + number[1:]
	}
	if len(number) < 8 || len(number) > 15 || number[0] == '0' {
		return "", false
	}
	if !strings.HasPrefix(value, "+") && !(len(number) == 11 && number[0] == '7') {
		return "", false
	}
	return "+" + number, true
}

func IsValidTelegramUsername(value string) bool {
	if len(value) > 64 {
		return false
	}
	for _, letter := range value {
		if (letter < 'A' || letter > 'Z') && (letter < 'a' || letter > 'z') &&
			(letter < '0' || letter > '9') && letter != '_' {
			return false
		}
	}
	return true
}

func IsValidMaxProfileURL(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 512 {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host != "max.ru" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	token, ok := strings.CutPrefix(u.Path, "/u/")
	if !ok || token == "" || len(token) > 256 {
		return false
	}
	for _, letter := range token {
		if (letter < 'A' || letter > 'Z') && (letter < 'a' || letter > 'z') &&
			(letter < '0' || letter > '9') && letter != '_' && letter != '-' {
			return false
		}
	}
	return true
}
