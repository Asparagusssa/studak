package maxbot

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"studak/internal/crud"
	"studak/internal/logx"
	"studak/internal/models"
)

const (
	channelMax        = "max"
	maxWebhookUpdates = 10

	stepStart      = "start"
	stepName       = "name"
	stepAge        = "age"
	stepGender     = "gender"
	stepUniversity = "university"
	stepSearchMode = "search_mode"
	stepPreferred  = "preferred"
	stepFilterMode = "filter_mode"
	stepFilterPref = "filter_preferred"
	stepBio        = "bio"
	stepMenu       = "menu"
)

var errInvalidUpdate = errors.New("invalid MAX update")

type Bot struct {
	DB     *gorm.DB
	Client *Client
	Secret string
	ctx    context.Context
}

func New(db *gorm.DB, token, secret string) *Bot {
	return &Bot{
		DB:     db,
		Client: NewClient(token),
		Secret: strings.TrimSpace(secret),
	}
}

func (b *Bot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	provided := r.Header.Get("X-Max-Bot-Api-Secret")
	if b.Secret == "" || len(provided) != len(b.Secret) || subtle.ConstantTimeCompare([]byte(provided), []byte(b.Secret)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("max webhook: read body: %v", err)
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	requestBot := *b
	requestBot.ctx = r.Context()
	requestBot.DB = b.DB.WithContext(r.Context())
	if err := requestBot.HandleBody(raw); err != nil {
		if errors.Is(err, errInvalidUpdate) {
			log.Printf("max webhook: invalid update: %v", err)
			http.Error(w, "invalid update", http.StatusBadRequest)
			return
		}
		logx.Error("MAX webhook processing failed", err)
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (b *Bot) HandleBody(raw []byte) error {
	updates := ParseUpdates(raw)
	if len(updates) == 0 {
		return fmt.Errorf("%w: empty or malformed body", errInvalidUpdate)
	}
	if len(updates) > maxWebhookUpdates {
		return fmt.Errorf("%w: batch contains %d updates, maximum is %d", errInvalidUpdate, len(updates), maxWebhookUpdates)
	}
	var first error
	for _, u := range updates {
		if err := b.HandleUpdate(u); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (b *Bot) HandleUpdate(u Update) error {
	switch u.UpdateType {
	case "bot_started":
		if u.User == nil {
			return fmt.Errorf("%w: bot_started without user", errInvalidUpdate)
		}
		maxID, err := u.User.MaxID()
		if err != nil {
			return fmt.Errorf("%w: %v", errInvalidUpdate, err)
		}
		return b.onStart(maxID, u.User.DisplayName())
	case "message_created":
		if u.Message == nil {
			return fmt.Errorf("%w: message_created without message", errInvalidUpdate)
		}
		if u.Message.Sender.IsBot {
			return nil
		}
		if u.Message.Recipient.ChatType != "" && u.Message.Recipient.ChatType != "dialog" {
			return nil
		}
		maxID, err := u.Message.Sender.MaxID()
		if err != nil {
			return fmt.Errorf("%w: %v", errInvalidUpdate, err)
		}
		return b.onText(maxID, u.Message.Body.Text)
	case "message_callback":
		if u.Callback == nil {
			return fmt.Errorf("%w: message_callback without callback", errInvalidUpdate)
		}
		maxID, err := u.Callback.User.MaxID()
		if err != nil {
			return fmt.Errorf("%w: %v", errInvalidUpdate, err)
		}
		if err := b.Client.AnswerCallbackContext(b.context(), u.Callback.CallbackID, ""); err != nil {
			logx.Error("MAX callback answer failed", err)
		}
		return b.onCallback(maxID, u.Callback.Payload)
	default:
		return nil
	}
}

func (b *Bot) onStart(maxID int64, displayName string) error {
	s, err := b.session(maxID)
	if err != nil {
		return err
	}
	if user, err := crud.GetUserByMaxID(b.DB, maxID); err == nil {
		id := user.ID
		s.UserID = &id
		s.Step = stepMenu
		if err := b.DB.Save(s).Error; err != nil {
			return err
		}
		return b.send(maxID, fmt.Sprintf("Снова привет, %s. Открываю меню.", user.Name), menuKeyboard())
	} else if !errors.Is(err, crud.ErrNotFound) {
		return err
	}
	s.Step = stepName
	if err := b.DB.Save(s).Error; err != nil {
		return err
	}
	hint := "Как тебя зовут?"
	if displayName != "" {
		hint = fmt.Sprintf("Как тебя зовут? В MAX указано «%s» — можешь отправить это имя или другое.", displayName)
	}
	return b.send(maxID, "Привет. Это студенческий дейтинг по вузам.\n\n"+hint, nil)
}

func (b *Bot) onText(maxID int64, text string) error {
	text = strings.TrimSpace(text)
	s, err := b.session(maxID)
	if err != nil {
		return err
	}
	lower := strings.ToLower(text)
	if lower == "/start" || lower == "старт" || lower == "начать" {
		name := ""
		if user, err := crud.GetUserByMaxID(b.DB, maxID); err == nil {
			name = user.Name
		}
		return b.onStart(maxID, name)
	}
	if s.Step == stepMenu || s.Step == "" {
		switch lower {
		case "анкеты", "смотреть", "лента":
			return b.showNext(s)
		case "профиль", "анкета":
			return b.showOwn(s)
		case "фильтр", "поиск":
			return b.beginFilter(s)
		}
	}

	switch s.Step {
	case stepName, stepStart, "":
		if text == "" {
			return b.send(maxID, "Напиши имя текстом.", nil)
		}
		if len([]rune(text)) > 100 {
			return b.send(maxID, "Имя слишком длинное. До 100 символов.", nil)
		}
		s.DraftName = text
		s.Step = stepAge
		if err := b.DB.Save(s).Error; err != nil {
			return err
		}
		return b.send(maxID, "Сколько тебе лет?", nil)
	case stepAge:
		age, err := strconv.Atoi(text)
		if err != nil || age < crud.MinAge || age > crud.MaxAge {
			return b.send(maxID, fmt.Sprintf("Нужно число от %d до %d.", crud.MinAge, crud.MaxAge), nil)
		}
		s.DraftAge = age
		s.Step = stepGender
		if err := b.DB.Save(s).Error; err != nil {
			return err
		}
		return b.askGender(maxID)
	case stepGender:
		return b.askGender(maxID)
	case stepUniversity, stepPreferred, stepFilterPref:
		return b.offerUniversities(s, text)
	case stepSearchMode, stepFilterMode:
		return b.askSearchMode(maxID)
	case stepBio:
		if len([]rune(text)) > 1000 {
			return b.send(maxID, "Слишком длинно. До 1000 символов, либо «-» чтобы пропустить.", nil)
		}
		if text != "-" && !strings.EqualFold(text, "пропустить") {
			s.DraftBio = text
		} else {
			s.DraftBio = ""
		}
		return b.finishOnboarding(s)
	case stepMenu:
		return b.send(maxID, "Выбери действие кнопкой или напиши: анкеты, профиль, фильтр.", menuKeyboard())
	default:
		s.Step = stepMenu
		_ = b.DB.Save(s)
		return b.send(maxID, "Открываю меню.", menuKeyboard())
	}
}

func (b *Bot) onCallback(maxID int64, payload string) error {
	s, err := b.session(maxID)
	if err != nil {
		return err
	}
	kind, arg, _ := strings.Cut(payload, ":")
	switch kind {
	case "gender":
		if s.Step != stepGender {
			return b.send(maxID, "Эта кнопка уже устарела. Продолжи с текущего шага.", nil)
		}
		if arg != "male" && arg != "female" && arg != "other" {
			return b.askGender(maxID)
		}
		s.DraftGender = arg
		s.Step = stepUniversity
		if err := b.DB.Save(s).Error; err != nil {
			return err
		}
		return b.send(maxID, "Напиши название или аббревиатуру своего вуза — например МГУ или ИТМО.", nil)
	case "uni":
		if s.Step != stepUniversity {
			return b.send(maxID, "Эта кнопка уже устарела. Продолжи с текущего шага.", nil)
		}
		id, err := strconv.ParseUint(arg, 10, 64)
		if err != nil {
			return b.send(maxID, "Не получилось выбрать вуз, попробуй ещё раз.", nil)
		}
		uid := uint(id)
		s.DraftUniversityID = &uid
		s.Step = stepSearchMode
		if err := b.DB.Save(s).Error; err != nil {
			return err
		}
		return b.askSearchMode(maxID)
	case "mode":
		if s.Step != stepSearchMode && s.Step != stepFilterMode {
			return b.send(maxID, "Эта кнопка уже устарела. Открой фильтр ещё раз.", menuKeyboard())
		}
		if !models.IsValidSearchMode(arg) {
			return b.askSearchMode(maxID)
		}
		filterOnly := s.Step == stepFilterMode
		s.DraftSearchMode = arg
		if arg == models.SearchModeSelected {
			if filterOnly {
				s.Step = stepFilterPref
			} else {
				s.Step = stepPreferred
			}
			s.DraftPreferred = ""
			if err := b.DB.Save(s).Error; err != nil {
				return err
			}
			return b.send(maxID, "Напиши вузы, в которых искать, по одному. Когда закончишь — нажми «Готово».", [][]Button{{CallbackButton("Готово", "pref_done:")}})
		}
		if filterOnly {
			return b.saveFilter(s)
		}
		s.Step = stepBio
		if err := b.DB.Save(s).Error; err != nil {
			return err
		}
		return b.send(maxID, "Коротко о себе (или «-», чтобы пропустить).", nil)
	case "pref":
		if s.Step != stepPreferred && s.Step != stepFilterPref {
			return b.send(maxID, "Эта кнопка уже устарела. Открой фильтр ещё раз.", menuKeyboard())
		}
		id, err := strconv.ParseUint(arg, 10, 64)
		if err != nil {
			return nil
		}
		var added bool
		s.DraftPreferred, added = appendID(s.DraftPreferred, uint(id))
		if !added {
			return b.send(maxID, fmt.Sprintf("Можно выбрать не больше %d вузов.", crud.MaxPreferredUniversities), [][]Button{{CallbackButton("Готово", "pref_done:")}})
		}
		s.Step = stepPreferred
		if err := b.DB.Save(s).Error; err != nil {
			return err
		}
		return b.send(maxID, "Добавлено. Ещё вуз текстом или «Готово».", [][]Button{{CallbackButton("Готово", "pref_done:")}})
	case "pref_done":
		if s.Step != stepPreferred && s.Step != stepFilterPref {
			return b.send(maxID, "Эта кнопка уже устарела. Открой фильтр ещё раз.", menuKeyboard())
		}
		if len(parseIDs(s.DraftPreferred)) == 0 {
			return b.send(maxID, "Добавь хотя бы один вуз поиском или смени режим.", [][]Button{{CallbackButton("Готово", "pref_done:")}})
		}
		if s.Step == stepFilterPref {
			return b.saveFilter(s)
		}
		s.Step = stepBio
		if err := b.DB.Save(s).Error; err != nil {
			return err
		}
		return b.send(maxID, "Коротко о себе (или «-», чтобы пропустить).", nil)
	case "like", "skip":
		return b.react(s, arg, kind == "like")
	case "menu":
		switch arg {
		case "feed":
			return b.showNext(s)
		case "profile":
			return b.showOwn(s)
		case "filter":
			return b.beginFilter(s)
		case "reset":
			clearDraft(s)
			s.Step = stepName
			_ = b.DB.Save(s)
			return b.send(maxID, "Ок, заполним анкету заново. Как тебя зовут?", nil)
		}
		s.Step = stepMenu
		_ = b.DB.Save(s)
		return b.send(maxID, "Меню:", menuKeyboard())
	default:
		return b.send(maxID, "Не понял кнопку. Открой меню.", menuKeyboard())
	}
}

func (b *Bot) askGender(maxID int64) error {
	return b.send(maxID, "Кто ты?", [][]Button{
		{CallbackButton("Парень", "gender:male"), CallbackButton("Девушка", "gender:female")},
		{CallbackButton("Другое", "gender:other")},
	})
}

func (b *Bot) askSearchMode(maxID int64) error {
	return b.send(maxID, "Где искать анкеты?", [][]Button{
		{CallbackButton("Только мой вуз", "mode:my_only")},
		{CallbackButton("Выбранные вузы", "mode:"+models.SearchModeSelected)},
		{CallbackButton("Все вузы", "mode:"+models.SearchModeAll)},
	})
}

func (b *Bot) offerUniversities(s *models.BotSession, query string) error {
	unis, err := crud.SearchUniversities(b.DB, query)
	if err != nil {
		return err
	}
	if len(unis) == 0 {
		return b.send(s.ExternalID, "Ничего не нашёл. Попробуй другое название, например МФТИ или ВШЭ.", nil)
	}
	prefix := "uni"
	if s.Step == stepPreferred || s.Step == stepFilterPref {
		prefix = "pref"
	}
	rows := make([][]Button, 0, len(unis)+1)
	for _, u := range unis {
		label := u.ShortName
		if label == "" {
			label = u.Name
		}
		if len([]rune(label)) > 32 {
			label = string([]rune(u.Name)[:32])
		}
		rows = append(rows, []Button{CallbackButton(label, fmt.Sprintf("%s:%d", prefix, u.ID))})
	}
	if s.Step == stepPreferred || s.Step == stepFilterPref {
		rows = append(rows, []Button{CallbackButton("Готово", "pref_done:")})
	}
	return b.send(s.ExternalID, "Выбери вуз:", rows)
}

func (b *Bot) finishOnboarding(s *models.BotSession) error {
	user, err := b.saveProfile(s)
	if err != nil {
		logx.Error("MAX profile save failed", err)
		return b.send(s.ExternalID, "Не получилось сохранить анкету. Попробуй ещё раз позже.", nil)
	}
	id := user.ID
	s.UserID = &id
	s.Step = stepMenu
	if err := b.DB.Save(s).Error; err != nil {
		return err
	}
	return b.send(s.ExternalID, "Анкета сохранена. Можно смотреть людей.", menuKeyboard())
}

func (b *Bot) saveProfile(s *models.BotSession) (*models.User, error) {
	mode := s.DraftSearchMode
	if mode == "" {
		mode = models.SearchModeAll
	}
	maxID := s.ExternalID
	prefs := parseIDs(s.DraftPreferred)
	if s.UserID != nil {
		in := crud.UpdateUserInput{
			Name:                   &s.DraftName,
			Age:                    &s.DraftAge,
			Gender:                 &s.DraftGender,
			Bio:                    &s.DraftBio,
			MyUniversityID:         s.DraftUniversityID,
			SearchMode:             &mode,
			PreferredUniversityIDs: &prefs,
		}
		return crud.UpdateUser(b.DB, *s.UserID, in)
	}
	user, err := crud.CreateUser(b.DB, crud.CreateUserInput{
		MaxID:                  &maxID,
		Name:                   s.DraftName,
		Age:                    s.DraftAge,
		Gender:                 s.DraftGender,
		Bio:                    s.DraftBio,
		MyUniversityID:         s.DraftUniversityID,
		SearchMode:             mode,
		PreferredUniversityIDs: prefs,
	})
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, crud.ErrAlreadyExists) {
		return nil, err
	}
	existing, err := crud.GetUserByMaxID(b.DB, maxID)
	if err != nil {
		return nil, err
	}
	s.UserID = &existing.ID
	return b.saveProfile(s)
}

func (b *Bot) beginFilter(s *models.BotSession) error {
	if _, err := b.appUser(s); err != nil {
		s.Step = stepName
		_ = b.DB.Save(s)
		return b.send(s.ExternalID, "Сначала заполним анкету. Как тебя зовут?", nil)
	}
	s.Step = stepFilterMode
	if err := b.DB.Save(s).Error; err != nil {
		return err
	}
	return b.askSearchMode(s.ExternalID)
}

func (b *Bot) saveFilter(s *models.BotSession) error {
	user, err := b.appUser(s)
	if err != nil {
		return err
	}
	mode := s.DraftSearchMode
	prefs := parseIDs(s.DraftPreferred)
	in := crud.UpdateUserInput{SearchMode: &mode}
	if mode == models.SearchModeSelected {
		in.PreferredUniversityIDs = &prefs
	}
	if _, err := crud.UpdateUser(b.DB, user.ID, in); err != nil {
		return err
	}
	s.Step = stepMenu
	if err := b.DB.Save(s).Error; err != nil {
		return err
	}
	return b.send(s.ExternalID, "Фильтр обновлён.", menuKeyboard())
}

func clearDraft(s *models.BotSession) {
	s.DraftName = ""
	s.DraftAge = 0
	s.DraftGender = ""
	s.DraftBio = ""
	s.DraftUniversityID = nil
	s.DraftSearchMode = ""
	s.DraftPreferred = ""
}

func (b *Bot) showOwn(s *models.BotSession) error {
	user, err := b.appUser(s)
	if err != nil {
		s.Step = stepName
		_ = b.DB.Save(s)
		return b.send(s.ExternalID, "Сначала заполним анкету. Как тебя зовут?", nil)
	}
	s.Step = stepMenu
	_ = b.DB.Save(s)
	return b.send(s.ExternalID, formatProfile(user)+"\n\nЭто ты.", menuKeyboard())
}

func (b *Bot) showNext(s *models.BotSession) error {
	user, err := b.appUser(s)
	if err != nil {
		s.Step = stepName
		_ = b.DB.Save(s)
		return b.send(s.ExternalID, "Сначала заполним анкету. Как тебя зовут?", nil)
	}
	next, err := crud.GetNextProfile(b.DB, user.ID)
	if errors.Is(err, crud.ErrNoProfiles) {
		s.Step = stepMenu
		_ = b.DB.Save(s)
		return b.send(s.ExternalID, "Пока больше никого нет. Зайди позже или смени фильтр.", menuKeyboard())
	}
	if err != nil {
		return err
	}
	s.Step = stepMenu
	_ = b.DB.Save(s)
	return b.send(s.ExternalID, formatProfile(next), [][]Button{{
		CallbackButton("Нравится", fmt.Sprintf("like:%d", next.ID)),
		CallbackButton("Дальше", fmt.Sprintf("skip:%d", next.ID)),
	}})
}

func (b *Bot) react(s *models.BotSession, rawID string, like bool) error {
	user, err := b.appUser(s)
	if err != nil {
		return b.showNext(s)
	}
	toID, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil || toID == 0 {
		return b.showNext(s)
	}
	res, err := crud.SetLike(b.DB, user.ID, uint(toID), like)
	if err != nil && !errors.Is(err, crud.ErrNotFound) {
		return err
	}
	if res != nil && res.IsMatch {
		other, _ := crud.GetUser(b.DB, uint(toID))
		name := "кто-то"
		if other != nil {
			name = other.Name
			if other.MaxID != nil && *other.MaxID != 0 {
				_ = b.send(*other.MaxID, fmt.Sprintf("Это взаимно: вас лайкнул(а) %s, %d.", user.Name, user.Age), nil)
			}
		}
		_ = b.send(s.ExternalID, fmt.Sprintf("Это взаимно! Вы и %s лайкнули друг друга.", name), nil)
	}
	return b.showNext(s)
}

func (b *Bot) appUser(s *models.BotSession) (*models.User, error) {
	if s.UserID != nil {
		return crud.GetUser(b.DB, *s.UserID)
	}
	user, err := crud.GetUserByMaxID(b.DB, s.ExternalID)
	if err != nil {
		return nil, err
	}
	id := user.ID
	s.UserID = &id
	_ = b.DB.Save(s)
	return user, nil
}

func (b *Bot) session(maxID int64) (*models.BotSession, error) {
	if maxID <= 0 {
		return nil, fmt.Errorf("invalid MAX user_id")
	}
	var s models.BotSession
	err := b.DB.Where("channel = ? AND external_id = ?", channelMax, maxID).First(&s).Error
	if err == nil {
		return &s, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	seed := models.BotSession{Channel: channelMax, ExternalID: maxID, Step: stepStart}
	if err := b.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel"}, {Name: "external_id"}},
		DoNothing: true,
	}).Create(&seed).Error; err != nil {
		return nil, err
	}
	err = b.DB.Where("channel = ? AND external_id = ?", channelMax, maxID).First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (b *Bot) send(userID int64, text string, keyboard [][]Button) error {
	if err := b.Client.SendContext(b.context(), userID, text, keyboard); err != nil {
		logx.Error(fmt.Sprintf("MAX send to user %d failed", userID), err)
		return err
	}
	return nil
}

func (b *Bot) context() context.Context {
	if b != nil && b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}

func menuKeyboard() [][]Button {
	return [][]Button{
		{CallbackButton("Смотреть анкеты", "menu:feed")},
		{CallbackButton("Моя анкета", "menu:profile"), CallbackButton("Фильтр", "menu:filter")},
		{CallbackButton("Заполнить заново", "menu:reset")},
	}
}

func formatProfile(u *models.User) string {
	uni := "вуз не указан"
	if u.MyUniversity != nil {
		if u.MyUniversity.ShortName != "" {
			uni = u.MyUniversity.ShortName
		} else {
			uni = u.MyUniversity.Name
		}
	}
	gender := ""
	switch u.Gender {
	case "male":
		gender = "парень"
	case "female":
		gender = "девушка"
	case "other":
		gender = "другое"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s, %d", u.Name, u.Age)
	if gender != "" {
		fmt.Fprintf(&b, " · %s", gender)
	}
	fmt.Fprintf(&b, "\n%s", uni)
	if strings.TrimSpace(u.Bio) != "" {
		fmt.Fprintf(&b, "\n\n%s", u.Bio)
	}
	return b.String()
}

func parseIDs(s string) []uint {
	if strings.TrimSpace(s) == "" {
		return []uint{}
	}
	parts := strings.Split(s, ",")
	out := make([]uint, 0, len(parts))
	seen := map[uint]struct{}{}
	for _, p := range parts {
		n, err := strconv.ParseUint(strings.TrimSpace(p), 10, 64)
		if err != nil || n == 0 {
			continue
		}
		id := uint(n)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func appendID(s string, id uint) (string, bool) {
	ids := parseIDs(s)
	for _, existing := range ids {
		if existing == id {
			return s, true
		}
	}
	if len(ids) >= crud.MaxPreferredUniversities {
		return s, false
	}
	ids = append(ids, id)
	parts := make([]string, 0, len(ids))
	seen := map[uint]struct{}{}
	for _, x := range ids {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		parts = append(parts, strconv.FormatUint(uint64(x), 10))
	}
	return strings.Join(parts, ","), true
}
