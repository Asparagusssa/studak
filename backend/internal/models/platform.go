package models

// Platform identifies a profile's own messenger. Legacy profiles with both
// messenger IDs are deliberately excluded from either bot's pool.
func (u *User) Platform() string {
	if u.TelegramID != nil && u.MaxID == nil {
		return "telegram"
	}
	if u.MaxID != nil && u.TelegramID == nil {
		return "max"
	}
	if u.TelegramID == nil && u.MaxID == nil {
		return "web"
	}
	return ""
}
