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
	stepWho        = "who"
	stepWhoFilter  = "who_filter"
	stepUniversity = "university"
	stepSearchMode = "search_mode"
	stepPreferred  = "preferred"
	stepFilterMode = "filter_mode"
	stepFilterPref = "filter_preferred"
	stepBio        = "bio"
	stepPhoto      = "photo"
	stepEditPhoto  = "edit_photo"
	stepMenu       = "menu"
	stepWrite      = "write"
)

var errInvalidUpdate = errors.New("invalid MAX update")

type Bot struct {
	DB        *gorm.DB
	Channel   string
	Messenger Messenger
	Peers     map[string]Messenger
	Client    *Client
	Secret    string
	ctx       context.Context
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
	if u.UpdateType == "message_callback" && u.Callback != nil {
		if u.Callback.User.IsBot || (u.Message != nil && u.Message.Recipient.ChatType != "" && u.Message.Recipient.ChatType != "dialog") {
			return nil
		}
	}
	if u.UpdateType == "bot_started" && u.User != nil && u.User.IsBot {
		return nil
	}
	return b.handleOnce(u)
}

func (b *Bot) handleUpdate(u Update) error {
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
		return b.onMessage(maxID, u.Message.Body)
	case "message_callback":
		if u.Callback == nil {
			return fmt.Errorf("%w: message_callback without callback", errInvalidUpdate)
		}
		maxID, err := u.Callback.User.MaxID()
		if err != nil {
			return fmt.Errorf("%w: %v", errInvalidUpdate, err)
		}
		if err := b.transport().AnswerCallbackContext(b.context(), u.Callback.CallbackID, ""); err != nil {
			logx.Error("MAX callback answer failed", err)
		}
		return b.onCallback(maxID, u.Callback.Payload)
	default:
		return nil
	}
}

const menuText = "Что хочешь сделать? ✨"
const staleButtonText = "Это кнопка из прошлого шага. Вот где мы остановились:"

func (b *Bot) onStart(maxID int64, displayName string) error {
	s, err := b.session(maxID)
	if err != nil {
		return err
	}
	if user, err := b.lookupUser(maxID); err == nil {
		id := user.ID
		s.UserID = &id
		s.Step = stepMenu
		s.PeerUserID = nil
		if err := b.saveSession(s); err != nil {
			return err
		}
		if user.Paused {
			return b.pausedMenu(maxID)
		}
		return b.send(maxID, fmt.Sprintf("С возвращением, %s! ✨\nХочешь посмотреть, кто есть в ленте?", user.Name), menuKeyboard())
	} else if !errors.Is(err, crud.ErrNotFound) {
		return err
	}
	if s.Step != stepStart && s.Step != "" && s.Step != stepMenu {
		return b.resume(s, "Продолжим с того места, где остановились.")
	}
	s.Step = stepName
	if err := b.saveSession(s); err != nil {
		return err
	}
	text := "Привет! Я Искра ✨\nЗдесь можно знакомиться со студентами своего вуза или искать в других. Заполни анкету, и я покажу, кто есть в ленте.\n\n1/8 · Как тебя зовут?"
	if displayName != "" {
		text += fmt.Sprintf("\nМожешь написать «%s» или выбрать другое имя.", displayName)
	}
	return b.send(maxID, text, nil)
}

func (b *Bot) onMessage(maxID int64, body Body) error {
	text := strings.TrimSpace(body.Text)
	s, err := b.session(maxID)
	if err != nil {
		return err
	}
	if body.Mid != "" {
		processed, err := b.relaySeen(body.Mid)
		if err != nil || processed {
			return err
		}
	}
	lower := strings.ToLower(text)
	if lower == "/cancel" && s.Step == stepWrite {
		return b.cancelWrite(s)
	}
	if lower == "/start" || lower == "/menu" || lower == "старт" || lower == "начать" {
		name := ""
		if user, err := b.lookupUser(maxID); err == nil {
			name = user.Name
		}
		return b.onStart(maxID, name)
	}
	if s.Step == stepWrite {
		return b.relayMessage(s, body)
	}
	if strings.HasPrefix(s.Step, deleteStepPrefix) {
		return b.sendDeleteConfirmation(s)
	}
	if s.Step == stepBlock {
		return b.resume(s, "")
	}
	token, images := body.imageToken()
	if s.Step == stepPhoto || s.Step == stepEditPhoto {
		if images > 1 {
			return b.askPhoto(s, "В анкете помещается одно фото. Отправь то, которое хочешь оставить.")
		}
		if images != 1 || token == "" || len(token) > 4096 {
			return b.askPhoto(s, "Пришли одно фото как изображение, через галерею.")
		}
		if s.Step == stepPhoto {
			return b.finishOnboarding(s, &token)
		}
		return b.updatePhoto(s, token)
	}
	if images > 0 {
		if s.Step == stepMenu {
			return b.send(maxID, "Чтобы поставить фото на анкету, открой «Моя анкета» → «Фото» и отправь его там.", menuKeyboard())
		}
		return b.resume(s, "К фото вернёмся в конце анкеты. Продолжим:")
	}
	if s.Step == stepMenu || s.Step == "" {
		switch lower {
		case "анкеты", "смотреть", "лента":
			return b.showNext(s)
		case "профиль", "анкета":
			return b.showOwn(s)
		case "фильтр", "поиск":
			return b.beginFilter(s)
		case "кого искать":
			return b.beginWhoFilter(s)
		case "кто меня лайкнул":
			return b.showIncoming(s)
		case "мэтчи", "мои мэтчи":
			return b.showMatches(s, 0)
		}
	}

	switch s.Step {
	case stepName, stepStart, "":
		if text == "" {
			return b.send(maxID, "1/8 · Напиши имя, которое увидят другие в анкете.", nil)
		}
		if len([]rune(text)) > 100 {
			return b.send(maxID, "Имя не поместилось. Напиши покороче — до 100 символов.", nil)
		}
		s.DraftName = text
		s.Step = stepAge
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.send(maxID, "2/8 · Сколько тебе лет? Напиши число, например 20.", nil)
	case stepAge:
		age, err := strconv.Atoi(text)
		if err != nil || age < crud.MinAge || age > crud.MaxAge {
			return b.send(maxID, fmt.Sprintf("Напиши возраст числом, например 20. Здесь можно указать от %d до %d лет.", crud.MinAge, crud.MaxAge), nil)
		}
		s.DraftAge = age
		s.Step = stepGender
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.askGender(maxID)
	case stepGender:
		return b.askGender(maxID)
	case stepWho, stepWhoFilter:
		return b.askWho(s)
	case stepUniversity, stepPreferred, stepFilterPref:
		return b.offerUniversities(s, text)
	case stepSearchMode, stepFilterMode:
		return b.askSearchMode(maxID, s.Step == stepFilterMode)
	case stepBio:
		if text == "" || len([]rune(text)) > 1000 {
			return b.send(maxID, "Напиши пару слов о себе (до 1000 символов) или нажми «Без описания».", bioKeyboard())
		}
		if text != "-" && !strings.EqualFold(text, "пропустить") {
			s.DraftBio = text
		} else {
			s.DraftBio = ""
		}
		return b.beginPhoto(s, stepPhoto)
	case stepMenu:
		return b.send(maxID, menuText, menuKeyboard())
	default:
		s.Step = stepMenu
		_ = b.saveSession(s)
		return b.send(maxID, menuText, menuKeyboard())
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
			return b.resume(s, staleButtonText)
		}
		if arg != "male" && arg != "female" && arg != "other" {
			return b.askGender(maxID)
		}
		s.DraftGender = arg
		s.Step = stepWho
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.askWho(s)
	case "who":
		if s.Step != stepWho && s.Step != stepWhoFilter {
			return b.resume(s, staleButtonText)
		}
		if !models.IsValidSearchGender(arg) {
			return b.askWho(s)
		}
		if s.Step == stepWhoFilter {
			return b.saveWhoFilter(s, arg)
		}
		s.DraftSearchGender = arg
		s.Step = stepUniversity
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.askUniversity(maxID, "")
	case "uni":
		if s.Step != stepUniversity {
			return b.resume(s, staleButtonText)
		}
		id, err := strconv.ParseUint(arg, 10, 64)
		if err != nil {
			return b.send(maxID, "Не получилось выбрать вуз. Напиши его название, чтобы обновить список.", nil)
		}
		uid := uint(id)
		s.DraftUniversityID = &uid
		s.Step = stepSearchMode
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.askSearchMode(maxID, false)
	case "mode":
		if s.Step != stepSearchMode && s.Step != stepFilterMode {
			return b.resume(s, staleButtonText)
		}
		if !models.IsValidSearchMode(arg) {
			return b.askSearchMode(maxID, s.Step == stepFilterMode)
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
			if err := b.saveSession(s); err != nil {
				return err
			}
			return b.askPreferred(s, "")
		}
		if filterOnly {
			return b.saveFilter(s)
		}
		s.Step = stepBio
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.askBio(maxID, "")
	case "pref":
		if s.Step != stepPreferred && s.Step != stepFilterPref {
			return b.resume(s, staleButtonText)
		}
		id, err := strconv.ParseUint(arg, 10, 64)
		if err != nil || id == 0 {
			return b.askPreferred(s, "Не получилось добавить вуз. Найди его ещё раз по названию.")
		}
		previous := s.DraftPreferred
		var added bool
		s.DraftPreferred, added = appendID(s.DraftPreferred, uint(id))
		if !added {
			return b.send(maxID, fmt.Sprintf("В списке уже %d вузов. Нажми «Готово», чтобы сохранить выбор.", crud.MaxPreferredUniversities), preferredKeyboard())
		}
		if previous == s.DraftPreferred {
			return b.askPreferred(s, "Этот вуз уже в списке.")
		}
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.askPreferred(s, fmt.Sprintf("Вуз добавлен · всего %d.", len(parseIDs(s.DraftPreferred))))
	case "pref_done":
		if s.Step != stepPreferred && s.Step != stepFilterPref {
			return b.resume(s, staleButtonText)
		}
		if len(parseIDs(s.DraftPreferred)) == 0 {
			return b.askPreferred(s, "Список пока пуст.")
		}
		if s.Step == stepFilterPref {
			return b.saveFilter(s)
		}
		s.Step = stepBio
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.askBio(maxID, "")
	case "pref_back":
		if s.Step == stepPreferred {
			s.Step = stepSearchMode
		} else if s.Step == stepFilterPref {
			s.Step = stepFilterMode
		} else {
			return b.resume(s, staleButtonText)
		}
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.askSearchMode(maxID, s.Step == stepFilterMode)
	case "bio_skip":
		if s.Step != stepBio {
			return b.resume(s, staleButtonText)
		}
		s.DraftBio = ""
		return b.beginPhoto(s, stepPhoto)
	case "photo":
		switch arg {
		case "skip":
			if s.Step == stepPhoto {
				return b.finishOnboarding(s, nil)
			}
		case "edit":
			if _, err := b.appUser(s); err != nil {
				if errors.Is(err, crud.ErrNotFound) {
					return b.resume(s, staleButtonText)
				}
				return err
			}
			return b.beginPhoto(s, stepEditPhoto)
		case "remove":
			if s.Step == stepEditPhoto {
				return b.updatePhoto(s, "")
			}
		}
		return b.resume(s, staleButtonText)
	case "privacy":
		return b.handlePrivacy(s, arg)
	case "more":
		return b.askBlock(s, arg)
	case "like", "skip", "like_back", "skip_in":
		return b.react(s, arg, kind == "like" || kind == "like_back", kind == "like_back" || kind == "skip_in")
	case "match":
		return b.showMatch(s, arg)
	case "write":
		return b.beginWrite(s, arg)
	case "cancel_write":
		if s.Step == stepWrite {
			return b.cancelWrite(s)
		}
		return b.resume(s, staleButtonText)
	case "matches":
		offset, err := strconv.Atoi(arg)
		if err != nil || offset < 0 || offset > 10000 {
			return b.showMatches(s, 0)
		}
		return b.showMatches(s, offset)
	case "menu":
		switch arg {
		case "feed":
			return b.showNext(s)
		case "profile":
			return b.showOwn(s)
		case "incoming":
			return b.showIncoming(s)
		case "filter":
			return b.beginFilter(s)
		case "who":
			return b.beginWhoFilter(s)
		case "reset":
			clearDraft(s)
			s.Step = stepName
			if err := b.saveSession(s); err != nil {
				return err
			}
			return b.send(maxID, "Старая анкета останется видна, пока не сохранишь новую.\n\n1/8 · Какое имя указать в анкете?", nil)
		case "home":
			return b.onStart(maxID, "")
		}
		return b.resume(s, staleButtonText)
	default:
		return b.resume(s, staleButtonText)
	}
}

func (b *Bot) askGender(maxID int64) error {
	return b.send(maxID, "3/8 · Как указать тебя в анкете?", [][]Button{
		{CallbackButton("Парень", "gender:male"), CallbackButton("Девушка", "gender:female")},
		{CallbackButton("Другое", "gender:other")},
	})
}

func (b *Bot) askSearchMode(maxID int64, filterOnly bool) error {
	text := "6/8 · Где хочешь знакомиться? Этот выбор можно поменять позже."
	if filterOnly {
		text = "Где искать новые анкеты? 🎓"
	}
	return b.send(maxID, text, [][]Button{
		{CallbackButton("В моём вузе", "mode:"+models.SearchModeMyOnly)},
		{CallbackButton("В выбранных вузах", "mode:"+models.SearchModeSelected)},
		{CallbackButton("Во всех вузах", "mode:"+models.SearchModeAll)},
	})
}

func (b *Bot) askUniversity(maxID int64, intro string) error {
	return b.send(maxID, withIntro(intro, "5/8 · В каком вузе ты учишься? Напиши название или сокращение, например МГУ или ИТМО."), nil)
}

func (b *Bot) askBio(maxID int64, intro string) error {
	return b.send(maxID, withIntro(intro, "7/8 · Хочешь рассказать о себе? Напиши, например, чем любишь заниматься после пар. Можно оставить анкету без описания."), bioKeyboard())
}

func (b *Bot) askPreferred(s *models.BotSession, intro string) error {
	return b.send(s.ExternalID, withIntro(intro, "Напиши название вуза и выбери его из списка. Так можно добавить несколько вузов. Когда закончишь, нажми «Готово»."), preferredKeyboard())
}

func preferredKeyboard() [][]Button {
	return [][]Button{{CallbackButton("Готово", "pref_done:"), CallbackButton("Назад к фильтрам", "pref_back:")}}
}

func bioKeyboard() [][]Button {
	return [][]Button{{CallbackButton("Без описания", "bio_skip:")}}
}

func withIntro(intro, text string) string {
	if intro == "" {
		return text
	}
	return intro + "\n\n" + text
}

func (b *Bot) resume(s *models.BotSession, intro string) error {
	id := s.ExternalID
	if strings.HasPrefix(s.Step, deleteStepPrefix) {
		return b.sendDeleteConfirmation(s)
	}
	switch s.Step {
	case stepBlock:
		if s.PeerUserID != nil {
			return b.askBlock(s, strconv.FormatUint(uint64(*s.PeerUserID), 10))
		}
		return b.send(id, menuText, menuKeyboard())
	case stepAge:
		return b.send(id, withIntro(intro, "2/8 · Сколько тебе лет? Напиши число, например 20."), nil)
	case stepGender:
		return b.askGender(id)
	case stepWho, stepWhoFilter:
		return b.askWho(s)
	case stepUniversity:
		return b.askUniversity(id, intro)
	case stepSearchMode, stepFilterMode:
		return b.askSearchMode(id, s.Step == stepFilterMode)
	case stepPreferred, stepFilterPref:
		return b.askPreferred(s, intro)
	case stepBio:
		return b.askBio(id, intro)
	case stepPhoto, stepEditPhoto:
		return b.askPhoto(s, intro)
	case stepWrite:
		return b.send(id, "Отправь текст для собеседника или нажми «Отмена».", writeKeyboard())
	case stepMenu:
		return b.send(id, menuText, menuKeyboard())
	default:
		return b.send(id, withIntro(intro, "1/8 · Как тебя зовут? Напиши имя для анкеты."), nil)
	}
}

func (b *Bot) offerUniversities(s *models.BotSession, query string) error {
	preferred := s.Step == stepPreferred || s.Step == stepFilterPref
	if query == "" || len([]rune(query)) > crud.MaxUniversitySearchRunes {
		if preferred {
			return b.askPreferred(s, "Нужно короткое название вуза, например МФТИ.")
		}
		return b.askUniversity(s.ExternalID, "Нужно короткое название вуза, например МФТИ.")
	}
	unis, err := crud.SearchUniversities(b.DB, query)
	if err != nil {
		return err
	}
	if len(unis) == 0 {
		text := "Не нашёл такой вуз. Попробуй сокращение или часть названия, например «МФТИ» или «Высшая школа»."
		if preferred {
			return b.send(s.ExternalID, text, preferredKeyboard())
		}
		return b.send(s.ExternalID, text, nil)
	}
	prefix := "uni"
	if preferred {
		prefix = "pref"
	}
	rows := make([][]Button, 0, len(unis)+1)
	for _, u := range unis {
		label := u.ShortName
		if label == "" {
			label = u.Name
		}
		if len([]rune(label)) > 32 {
			label = string([]rune(label)[:31]) + "…"
		}
		rows = append(rows, []Button{CallbackButton(label, fmt.Sprintf("%s:%d", prefix, u.ID))})
	}
	if preferred {
		rows = append(rows, preferredKeyboard()...)
	}
	return b.send(s.ExternalID, "Нашёл такие варианты. Выбери нужный вуз:", rows)
}

func (b *Bot) finishOnboarding(s *models.BotSession, photoToken *string) error {
	user, err := b.saveProfile(s, photoToken)
	if err != nil {
		logx.Error("MAX profile save failed", err)
		return b.askPhoto(s, "Не удалось сохранить анкету. Попробуй отправить фото ещё раз или продолжить без него.")
	}
	id := user.ID
	s.UserID = &id
	s.Step = stepMenu
	if err := b.saveSession(s); err != nil {
		return err
	}
	text := "Анкета готова! ✨\nМожешь посмотреть, кто есть в ленте, или сначала открыть свою анкету."
	if photoToken != nil {
		text = "Анкета с фото готова! ✨\nМожешь посмотреть, кто есть в ленте, или сначала открыть свою анкету."
	}
	return b.send(s.ExternalID, text, menuKeyboard())
}

func (b *Bot) saveProfile(s *models.BotSession, photoToken *string) (*models.User, error) {
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
			MaxPhotoToken:          b.photoUpdate(photoToken).MaxPhotoToken,
			TelegramPhotoID:        b.photoUpdate(photoToken).TelegramPhotoID,
			MyUniversityID:         s.DraftUniversityID,
			SearchMode:             &mode,
			PreferredUniversityIDs: &prefs,
		}
		if s.DraftSearchGender != "" {
			in.SearchGender = &s.DraftSearchGender
		}
		return crud.UpdateUser(b.DB, *s.UserID, in)
	}
	in := crud.CreateUserInput{
		MaxID:                  &maxID,
		Name:                   s.DraftName,
		Age:                    s.DraftAge,
		Gender:                 s.DraftGender,
		Bio:                    s.DraftBio,
		MyUniversityID:         s.DraftUniversityID,
		SearchMode:             mode,
		SearchGender:           s.DraftSearchGender,
		PreferredUniversityIDs: prefs,
	}
	if b.channel() == "telegram" {
		in.TelegramID = &maxID
		in.MaxID = nil
	}
	if photoToken != nil {
		if b.channel() == "telegram" {
			in.TelegramPhotoID = *photoToken
		} else {
			in.MaxPhotoToken = *photoToken
		}
	}
	user, err := crud.CreateUser(b.DB, in)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, crud.ErrAlreadyExists) {
		return nil, err
	}
	existing, err := b.lookupUser(maxID)
	if err != nil {
		return nil, err
	}
	s.UserID = &existing.ID
	return b.saveProfile(s, photoToken)
}

func (b *Bot) beginFilter(s *models.BotSession) error {
	if _, err := b.appUser(s); err != nil {
		s.Step = stepName
		_ = b.saveSession(s)
		return b.send(s.ExternalID, "Чтобы начать знакомства, нужно заполнить анкету.\n\n1/8 · Как тебя зовут?", nil)
	}
	s.Step = stepFilterMode
	if err := b.saveSession(s); err != nil {
		return err
	}
	return b.askSearchMode(s.ExternalID, true)
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
	if err := b.saveSession(s); err != nil {
		return err
	}
	text := "Теперь в ленте будут анкеты из всех вузов."
	switch mode {
	case models.SearchModeMyOnly:
		text = "Теперь в ленте будут анкеты только из твоего вуза."
	case models.SearchModeSelected:
		text = "Теперь в ленте будут анкеты из выбранных вузов."
	}
	return b.send(s.ExternalID, text, menuKeyboard())
}

func clearDraft(s *models.BotSession) {
	s.DraftName = ""
	s.DraftAge = 0
	s.DraftGender = ""
	s.DraftSearchGender = ""
	s.DraftBio = ""
	s.DraftUniversityID = nil
	s.DraftSearchMode = ""
	s.DraftPreferred = ""
}

func (b *Bot) showOwn(s *models.BotSession) error {
	user, err := b.appUser(s)
	if err != nil {
		s.Step = stepName
		_ = b.saveSession(s)
		return b.send(s.ExternalID, "Чтобы начать знакомства, нужно заполнить анкету.\n\n1/8 · Как тебя зовут?", nil)
	}
	s.Step = stepMenu
	_ = b.saveSession(s)
	return b.sendOwn(s, user, "Твоя анкета ✨")
}

func (b *Bot) showNext(s *models.BotSession) error {
	user, err := b.appUser(s)
	if err != nil {
		s.Step = stepName
		_ = b.saveSession(s)
		return b.send(s.ExternalID, "Чтобы начать знакомства, нужно заполнить анкету.\n\n1/8 · Как тебя зовут?", nil)
	}
	if err := b.leaveWrite(s); err != nil {
		return err
	}
	next, err := crud.GetNextProfile(b.DB, user.ID)
	if errors.Is(err, crud.ErrProfilePaused) {
		return b.pausedMenu(s.ExternalID)
	}
	if errors.Is(err, crud.ErrNoProfiles) {
		s.Step = stepMenu
		_ = b.saveSession(s)
		return b.send(s.ExternalID, "Новых анкет по твоим настройкам пока нет. Попробуй расширить поиск или загляни позже.", [][]Button{
			{CallbackButton("Изменить вузы", "menu:filter"), CallbackButton("Кого искать", "menu:who")},
			{CallbackButton("Проверить ещё раз", "menu:feed"), CallbackButton("В меню", "menu:home")},
		})
	}
	if err != nil {
		return err
	}
	s.Step = stepMenu
	_ = b.saveSession(s)
	return b.sendProfile(s.ExternalID, next, "", [][]Button{
		{CallbackButton("💛 Нравится", fmt.Sprintf("like:%d", next.ID)), CallbackButton("Пропустить", fmt.Sprintf("skip:%d", next.ID))},
		{moreButton(next.ID), CallbackButton("В меню", "menu:home")},
	})
}

func (b *Bot) appUser(s *models.BotSession) (*models.User, error) {
	if s.UserID != nil {
		return crud.GetUser(b.DB, *s.UserID)
	}
	user, err := b.lookupUser(s.ExternalID)
	if err != nil {
		return nil, err
	}
	id := user.ID
	s.UserID = &id
	_ = b.saveSession(s)
	return user, nil
}

func (b *Bot) session(maxID int64) (*models.BotSession, error) {
	if maxID <= 0 {
		return nil, fmt.Errorf("invalid MAX user_id")
	}
	var s models.BotSession
	err := b.DB.Where("channel = ? AND external_id = ?", b.channel(), maxID).First(&s).Error
	if err == nil {
		return &s, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	seed := models.BotSession{Channel: b.channel(), ExternalID: maxID, Step: stepStart}
	if err := b.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel"}, {Name: "external_id"}},
		DoNothing: true,
	}).Create(&seed).Error; err != nil {
		return nil, err
	}
	err = b.DB.Where("channel = ? AND external_id = ?", b.channel(), maxID).First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// A stale webhook can update a session, but it must not recreate a session
// that was removed when its owner deleted the profile.
func (b *Bot) saveSession(s *models.BotSession) error {
	return b.DB.Model(s).Select("*").Omit("id", "created_at").Updates(s).Error
}

func (b *Bot) send(userID int64, text string, keyboard [][]Button) error {
	if err := b.transport().SendContext(b.context(), userID, text, keyboard); err != nil {
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
		{CallbackButton("✨ Смотреть анкеты", "menu:feed")},
		{CallbackButton("💌 Входящие лайки", "menu:incoming"), CallbackButton("Мои мэтчи", "matches:0")},
		{CallbackButton("Моя анкета", "menu:profile")},
		{CallbackButton("Вузы для поиска", "menu:filter"), CallbackButton("Кого искать", "menu:who")},
		{CallbackButton("⚙️ Настройки", "privacy:home")},
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
	fmt.Fprintf(&b, "\n🎓 %s", uni)
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
