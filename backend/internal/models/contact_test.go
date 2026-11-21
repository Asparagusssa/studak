package models

import "testing"

func TestNormalizeMaxContact(t *testing.T) {
	for input, expected := range map[string]string{
		"+7 (999) 123-45-67":              "+79991234567",
		"8 (999) 123-45-67":               "+79991234567",
		"79991234567":                     "+79991234567",
		"+44 7911 123456":                 "+447911123456",
		"https://t.me/alice_test":         "https://t.me/alice_test",
		"@alice_test":                     "https://t.me/alice_test",
		"t.me/alice_test":                 "https://t.me/alice_test",
		"https://max.ru/u/opaque_ABC-123": "https://max.ru/u/opaque_ABC-123",
	} {
		if got, valid := NormalizeMaxContact(input); !valid || got != expected {
			t.Errorf("%q: got %q, valid=%v; want %q", input, got, valid, expected)
		}
	}
	for _, input := range []string{"", "пропустить", "123", "+0123456789", "+1234567890123456", "https://t.me/+invite", "https://t.me/share", "https://t.me/username?start=x", "https://max.ru.evil.test/u/x", "https://max.ru/join/group", "https://evil.test/username"} {
		if got, valid := NormalizeMaxContact(input); valid {
			t.Errorf("invalid contact accepted: %q -> %q", input, got)
		}
	}
}
