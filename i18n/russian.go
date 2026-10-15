package i18n

import "strconv"

// russian is the Russian preset.
//
// Strings that embed a value use a colon form («Редактирование: Записи») rather
// than a sentence, because a resource title arrives in whatever case the
// consumer wrote it in and a sentence would need it declined.
//
//nolint:gosec // G101 false positive: UI copy that mentions "password", not a credential
var russian = Strings{
	Lang: "ru",

	Logout:             "Выйти",
	Settings:           "Настройки",
	ToggleSidebar:      "Свернуть или развернуть меню",
	OpenNavigation:     "Открыть меню",
	SwitchToLightTheme: "Включить светлую тему",
	SwitchToDarkTheme:  "Включить тёмную тему",

	LoginTitle:   "Вход в админку",
	LoginHeading: "Админка",
	LoginUser:    "Логин",
	LoginEmail:   "Эл. почта",
	Password:     "Пароль",
	ShowPassword: "Показать пароль",
	HidePassword: "Скрыть пароль",
	SignIn:       "Войти",

	ErrInvalidUsernameOrPassword: "Неверный логин или пароль",
	ErrInvalidEmailOrPassword:    "Неверная почта или пароль",
	ErrTooManyLoginAttempts:      "Слишком много попыток входа. Повторите позже.",
	ErrTooManyCodeAttempts:       "Слишком много попыток проверки. Повторите позже.",
	Err2FAUnavailable:            "Двухфакторная аутентификация недоступна",
	ErrInvalidSession:            "Недействительная сессия или аккаунт",
	ErrInvalidCode:               "Неверный код или резервный код",
	ErrCodeRequired:              "Введите код подтверждения или резервный код",

	TwoFactorTitle: "Двухфакторная аутентификация",
	TwoFactorCode:  "Код подтверждения или резервный код",
	Verify:         "Подтвердить",

	IndexHeading: "Админка",
	IndexEmpty:   "Разделы ещё не добавлены.",

	Total:             func(n int) string { return "Всего: " + strconv.Itoa(n) },
	New:               "+ Создать",
	Edit:              "Изменить",
	Delete:            "Удалить",
	Undo:              "Вернуть",
	DeleteRowLabel:    func(id string) string { return "Удалить запись " + id },
	Deleted:           func(label string) string { return "Удалено: " + label },
	SearchPlaceholder: func(caption string) string { return "Поиск: " + caption + "…" },
	ViewsLabel:        "вид",
	NoResults:         "Ничего не найдено",
	Prev:              "Назад",
	Next:              "Далее",
	LoadMore:          "Показать ещё",

	EditTitle:           func(title string) string { return "Редактирование: " + title },
	NewTitle:            func(title string) string { return "Создание: " + title },
	BackToList:          "Назад к списку",
	SaveChanges:         "Сохранить",
	Create:              "Создать",
	Cancel:              "Отмена",
	LocaleSwitcherLabel: "Язык",
	LocaleEditingNote: func(active, def string) string {
		return "Редактируется перевод " + active + ". Общие поля редактируются в версии " + def + "."
	},
	SelectPlaceholder: "— выберите —",

	FieldRequired:  func(label string) string { return label + ": обязательное поле" },
	FieldNotNumber: func(label string) string { return label + ": должно быть числом" },
	FieldNotDate:   func(label string) string { return label + ": неверная дата (ГГГГ-ММ-ДД)" },
	FieldNotDateTime: func(label string) string {
		return label + ": неверные дата и время (ГГГГ-ММ-ДД ЧЧ:ММ)"
	},
	FieldNotChoice: func(label string) string { return label + ": недопустимое значение" },
	FieldNotJSON:   func(label string) string { return label + ": некорректный JSON" },

	BackTo:   func(title string) string { return "Назад к разделу: " + title },
	NoDetail: "Нет данных для отображения.",
	ConfirmDelete: func(string) string {
		return "Удалить запись? Это действие нельзя отменить."
	},

	TrashTitle:      "Корзина",
	TrashIntro:      "Удалённые записи сохраняются и не уничтожаются. Их можно вернуть здесь в любой момент.",
	TrashEmpty:      "Корзина пуста.",
	TrashUnreadable: "Не удалось прочитать корзину этого раздела. Записи на месте — подробности в журнале сервера.",
	TrashShowing: func(shown, total int) string {
		return "Показаны последние удалённые: " + strconv.Itoa(shown) + " из " + strconv.Itoa(total) + "."
	},
	RestoreRowLabel: func(id string) string { return "Вернуть запись " + id },
	SystemGroup:     "Система",

	TOTPSetupTitle:        "Настройка двухфакторной аутентификации",
	TOTPScanPrompt:        "Отсканируйте код в приложении-аутентификаторе (Google Authenticator, 1Password, Authy, …):",
	TOTPQRAlt:             "QR-код для настройки двухфакторной аутентификации: отсканируйте его в приложении-аутентификаторе или введите код вручную ниже",
	TOTPManualEntry:       "Не получается отсканировать? Введите этот код вручную:",
	TOTPURIPrompt:         "Полная ссылка для настройки (некоторые приложения принимают её целиком):",
	TOTPCodeLabel:         "Введите 6-значный код из приложения",
	ConfirmPassword:       "Подтвердите текущий пароль",
	TOTPVerifyEnable:      "Проверить и включить",
	TOTPAlreadyEnabled:    "Двухфакторная аутентификация уже включена для этого аккаунта.",
	TOTPDisable:           "Отключить двухфакторную аутентификацию",
	BackToDashboard:       "Назад на главную",
	RecoverySave:          "Сохраните резервные коды сейчас.",
	RecoveryNote:          "Каждый код работает один раз — на случай, если вы потеряете доступ к приложению-аутентификатору. Повторно они показаны не будут.",
	TOTPEnabledTitle:      "Двухфакторная аутентификация включена",
	TOTPEnabledBody:       "Ваш аккаунт теперь защищён двухфакторной аутентификацией.",
	ContinueToDashboard:   "Перейти на главную",
	TOTPDisabledTitle:     "Двухфакторная аутентификация отключена",
	TOTPDisabledBody:      "Двухфакторная аутентификация для этого аккаунта отключена.",
	RecoveryRegenTitle:    "Резервные коды обновлены",
	RecoveryRegenBody:     "Прежние резервные коды больше не действуют.",
	Continue:              "Продолжить",
	TOTPDisableTitle:      "Отключение двухфакторной аутентификации",
	RecoveryRegenerate:    "Обновление резервных кодов",
	RecoveryRegenerateBtn: "Обновить коды",
	ErrIncorrectPassword:  "Неверный пароль.",
	ErrTOTPInvalidCode:    "Неверный или уже использованный код. Введите текущий код из приложения-аутентификатора.",
}

// Russian returns the Russian preset. Hand it to Config.Strings as is, or
// override single fields on the returned value first.
func Russian() Strings { return russian }
