package models

import (
	"net/url"
	"strings"
)

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
