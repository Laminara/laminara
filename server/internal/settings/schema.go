package settings

import (
	"github.com/laminara/laminara/server/internal/access"
	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/auth/hash"
	_ "github.com/laminara/laminara/server/internal/auth/providers"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/news"
	"github.com/laminara/laminara/server/internal/skin"
	"github.com/laminara/laminara/server/internal/storage"
)

type Kind string

const (
	KindText     Kind = "text"
	KindSecret   Kind = "secret"
	KindBool     Kind = "bool"
	KindInt      Kind = "int"
	KindDuration Kind = "duration"
	KindChoice   Kind = "choice"
	KindList     Kind = "list"
	KindPairs    Kind = "pairs"
	KindJSON     Kind = "json"
)

type Field struct {
	Key       string
	Label     string
	Hint      string
	Kind      Kind
	Default   string
	Options   func() []string
	Variants  map[string][]Field
	VariantOf string
}

type Section struct {
	Key         string
	Title       string
	Hint        string
	Fields      []Field
	Collections []Collection
}

type Collection struct {
	Key       string
	Title     string
	Hint      string
	Keyed     bool
	NameLabel string
	NameHint  string
	Fields    []Field
}

var sqlDrivers = func() []string { return []string{"postgres", "mysql", "sqlite"} }

var schema = []Section{
	{
		Key:   "api",
		Title: "Публичный слушатель",
		Hint:  "Порт, на который приходит лаунчер: манифесты, файлы сборок и вход в игре.",
		Fields: []Field{
			{Key: "addr", Label: "Адрес и порт", Kind: KindText, Default: "0.0.0.0:8099", Hint: "За nginx держите 127.0.0.1:8099 — наружу смотрит только nginx."},
			{Key: "xAccel", Label: "Файлы отдаёт nginx", Kind: KindBool, Default: "false", Hint: "Включайте только вместе с nginx: сервер отвечает заголовком, а байты шлёт nginx."},
		},
	},
	{
		Key:   "auth",
		Title: "Аккаунты игроков",
		Hint:  "Откуда брать логины и пароли. Laminara держит сессии, но не хранит самих игроков.",
		Fields: []Field{
			{Key: "provider", Label: "Источник аккаунтов", Kind: KindChoice, Default: "jsonfile", Options: auth.ProviderNames, Hint: "jsonfile — файл рядом с сервером, sql — ваша база, http — ваш сайт."},
			{Key: "accessTTL", Label: "Срок токена доступа", Kind: KindDuration, Default: "15m", Hint: "Через сколько лаунчер обновляет доступ. Больше срок — реже запросы, дольше живёт украденный токен."},
			{Key: "refreshTTL", Label: "Срок токена обновления", Kind: KindDuration, Default: "720h", Hint: "Сколько игрок остаётся в лаунчере без ввода пароля."},
			{Key: "sessions.backend", Label: "Где хранить сессии", Kind: KindChoice, Default: "memory", Options: func() []string { return []string{"memory", "redis"} }, Hint: "memory — сессии теряются при перезапуске; redis — переживают его."},
			{Key: "sessions.redisAddr", Label: "Адрес Redis", Kind: KindText, Default: "127.0.0.1:6379", Hint: "Нужен, только когда сессии в Redis."},
			{Key: "config", Label: "Настройки источника", VariantOf: "provider", Variants: map[string][]Field{
				"jsonfile": {
					{Key: "path", Label: "Файл с аккаунтами", Kind: KindText, Hint: "JSON-массив записей вида {\"username\":\"…\",\"password\":\"…\"}."},
					{Key: "hash", Label: "Как захеширован пароль", Kind: KindChoice, Default: "argon2id", Options: hash.Names},
					{Key: "fields.username", Label: "Поле логина", Kind: KindText, Default: "username"},
					{Key: "fields.password", Label: "Поле пароля", Kind: KindText, Default: "password"},
					{Key: "fields.uuid", Label: "Поле UUID", Kind: KindText, Hint: "Пусто — UUID посчитается из логина."},
				},
				"sql": {
					{Key: "driver", Label: "СУБД", Kind: KindChoice, Options: sqlDrivers},
					{Key: "dsn", Label: "Строка подключения", Kind: KindSecret, Hint: "Например postgres://user:pass@host:5432/base?sslmode=disable"},
					{Key: "table", Label: "Таблица", Kind: KindText, Default: "users"},
					{Key: "hash", Label: "Как захеширован пароль", Kind: KindChoice, Default: "bcrypt", Options: hash.Names},
					{Key: "fields.username", Label: "Колонка логина", Kind: KindText, Default: "username"},
					{Key: "fields.password", Label: "Колонка пароля", Kind: KindText, Default: "password"},
					{Key: "fields.uuid", Label: "Колонка UUID", Kind: KindText},
					{Key: "query", Label: "Свой запрос", Kind: KindText, Hint: "Если задан, таблица и колонки не используются."},
				},
				"http": {
					{Key: "url", Label: "Адрес проверки пароля", Kind: KindText, Hint: "Ваш сайт получает логин и пароль и отвечает JSON."},
					{Key: "usernameField", Label: "Поле логина в запросе", Kind: KindText, Default: "username"},
					{Key: "passwordField", Label: "Поле пароля в запросе", Kind: KindText, Default: "password"},
					{Key: "uuidField", Label: "Поле UUID в ответе", Kind: KindText, Default: "uuid"},
					{Key: "successField", Label: "Поле успеха в ответе", Kind: KindText, Default: "ok"},
				},
			}},
		},
	},
	{
		Key:   "storage",
		Title: "Хранилище файлов",
		Hint:  "Где лежат файлы сборок, которые качает лаунчер.",
		Fields: []Field{
			{Key: "backend", Label: "Где хранить", Kind: KindChoice, Default: "fs", Options: storage.BackendNames, Hint: "fs — диск этого сервера, s3 — любое S3-совместимое хранилище."},
			{Key: "config", Label: "Настройки хранилища", VariantOf: "backend", Variants: map[string][]Field{
				"fs": {
					{Key: "root", Label: "Папка объектов", Kind: KindText, Hint: "Сюда складываются файлы всех сборок."},
					{Key: "xaccelPrefix", Label: "Внутренний префикс nginx", Kind: KindText, Default: "/_objects", Hint: "Должен совпадать с локацией в конфиге nginx."},
				},
				"s3": {
					{Key: "endpoint", Label: "Адрес хранилища", Kind: KindText},
					{Key: "region", Label: "Регион", Kind: KindText, Default: "us-east-1"},
					{Key: "bucket", Label: "Бакет", Kind: KindText},
					{Key: "accessKeyId", Label: "Ключ доступа", Kind: KindText},
					{Key: "secretAccessKey", Label: "Секрет", Kind: KindSecret},
					{Key: "pathStyle", Label: "Путь вместо поддомена", Kind: KindBool, Default: "false", Hint: "Нужен своим хранилищам вроде Garage и MinIO."},
					{Key: "secure", Label: "Только HTTPS", Kind: KindBool, Default: "true"},
				},
			}},
		},
	},
	{
		Key:   "build",
		Title: "Сборки и подпись",
		Hint:  "Где живут профили сборок и какими ключами подписаны манифесты.",
		Fields: []Field{
			{Key: "profilesDir", Label: "Папка сборок", Kind: KindText, Hint: "В ней install собирает клиент, оттуда publish снимает манифест."},
			{Key: "signingKeyPath", Label: "Ключ подписи", Kind: KindText, Hint: "Создаётся сам при первом запуске. Потеря ключа — потеря доверия старых лаунчеров."},
			{Key: "trustedSigningKeys", Label: "Ещё доверенные ключи", Kind: KindList, Hint: "Пути к ключам, которые лаунчеры продолжают принимать. Через запятую."},
		},
	},
	{
		Key:   "yggdrasil",
		Title: "Вход в игре и скины",
		Hint:  "Игра проверяет вход через authlib-injector на этом же адресе.",
		Fields: []Field{
			{Key: "enabled", Label: "Вход в игре включён", Kind: KindBool, Default: "false", Hint: "Без него игроки не смогут войти даже в лаунчер."},
			{Key: "serverName", Label: "Имя в окне игры", Kind: KindText, Default: "Laminara"},
			{Key: "rsaKeyPath", Label: "Ключ подписи скинов", Kind: KindText, Hint: "Создаётся сам при первом запуске."},
			{Key: "skinDomains", Label: "Домены скинов", Kind: KindList, Hint: "Откуда игре разрешено брать картинки скинов. Через запятую."},
			{Key: "skinProvider", Label: "Откуда брать скины", Kind: KindChoice, Default: "template", Options: skin.ProviderNames},
			{Key: "skinConfig", Label: "Настройки скинов", VariantOf: "skinProvider", Variants: map[string][]Field{
				"template": {
					{Key: "skin", Label: "Ссылка на скин", Kind: KindText, Hint: "Подставляются %nickname% и %uuid%."},
					{Key: "cape", Label: "Ссылка на плащ", Kind: KindText},
					{Key: "slim", Label: "Тонкие руки", Kind: KindBool, Default: "false"},
				},
				"json": {
					{Key: "url", Label: "Адрес JSON со скинами", Kind: KindText, Hint: "Подставляются %nickname% и %uuid%."},
				},
				"sql": {
					{Key: "driver", Label: "СУБД", Kind: KindChoice, Options: sqlDrivers},
					{Key: "dsn", Label: "Строка подключения", Kind: KindSecret},
					{Key: "table", Label: "Таблица", Kind: KindText},
					{Key: "lookup", Label: "По чему искать", Kind: KindChoice, Default: "username", Options: func() []string { return []string{"username", "uuid"} }},
					{Key: "fields.username", Label: "Колонка логина", Kind: KindText, Default: "username"},
					{Key: "fields.uuid", Label: "Колонка UUID", Kind: KindText, Default: "uuid"},
					{Key: "fields.skin", Label: "Колонка скина", Kind: KindText, Default: "skin"},
					{Key: "fields.cape", Label: "Колонка плаща", Kind: KindText, Default: "cape"},
					{Key: "fields.model", Label: "Колонка модели", Kind: KindText, Default: "model"},
					{Key: "query", Label: "Свой запрос", Kind: KindText},
					{Key: "slim", Label: "Тонкие руки по умолчанию", Kind: KindBool, Default: "false"},
				},
			}},
		},
	},
	{
		Key:   "hwid",
		Title: "Распознавание компьютеров",
		Hint:  "Лаунчер присылает признаки железа — по ним узнаются вернувшиеся нарушители.",
		Fields: []Field{
			{Key: "mode", Label: "Режим", Kind: KindChoice, Default: "observe", Options: func() []string {
				return []string{string(hwid.ModeOff), string(hwid.ModeObserve), string(hwid.ModeEnforce)}
			}, Hint: "off — выключено, observe — только запоминаем, enforce — баны применяются."},
			{Key: "store.backend", Label: "Где хранить компьютеры", Kind: KindChoice, Default: "memory", Options: hwid.StoreNames},
			{Key: "store.config", Label: "Настройки хранилища", VariantOf: "store.backend", Variants: map[string][]Field{
				"sql": {
					{Key: "driver", Label: "СУБД", Kind: KindChoice, Default: "sqlite", Options: sqlDrivers},
					{Key: "dsn", Label: "Строка подключения", Kind: KindSecret, Hint: "Для sqlite — путь к файлу базы."},
				},
			}},
			{Key: "requireReport", Label: "Требовать отчёт о компьютере", Kind: KindBool, Default: "false", Hint: "Без отчёта лаунчер не пустят в игру."},
			{Key: "requireChallenge", Label: "Требовать подпись железа", Kind: KindBool, Default: "true", Hint: "Отчёт подписывается ключом машины — подделать сложнее."},
			{Key: "requireLauncher", Label: "Вход в игре только через лаунчер", Kind: KindBool, Default: "false"},
			{Key: "requireHardwareKey", Label: "Требовать аппаратный ключ", Kind: KindBool, Default: "false", Hint: "Отсекает машины без TPM — в том числе честных игроков на старом железе."},
			{Key: "minScore", Label: "Порог «тот же компьютер»", Kind: KindInt, Default: "45", Hint: "Вес совпавших признаков, при котором это та же машина."},
			{Key: "minKinds", Label: "Сколько признаков должно совпасть", Kind: KindInt, Default: "2"},
			{Key: "clusterScore", Label: "Порог «родственный компьютер»", Kind: KindInt, Default: "25", Hint: "Ниже порога машины считаются одной группой — так ловятся переустановки."},
			{Key: "fanOutLimit", Label: "Признак обесценивается после", Kind: KindInt, Default: "25", Hint: "Если признак встречается на стольких машинах, он перестаёт что-либо значить."},
			{Key: "maxClusterSize", Label: "Предел группы для бана", Kind: KindInt, Default: "8", Hint: "Группу больше этой автобан не трогает — банится только аккаунт."},
			{Key: "vmPolicy", Label: "Виртуальные машины", Kind: KindChoice, Default: "flag", Options: func() []string { return []string{string(hwid.VMAllow), string(hwid.VMFlag), string(hwid.VMDeny)} }, Hint: "allow — пускать молча, flag — пометить, deny — не пускать."},
			{Key: "hardwareBanTTL", Label: "Срок бана по железу", Kind: KindDuration, Default: "2160h"},
			{Key: "ticketTTL", Label: "Срок пропуска в игру", Kind: KindDuration, Default: "10m"},
			{Key: "challengeTTL", Label: "Срок задания на подпись", Kind: KindDuration, Default: "2m"},
			{Key: "retention", Label: "Хранить записи о компьютерах", Kind: KindDuration, Default: "4320h"},
			{Key: "ipRetention", Label: "Хранить адреса входа", Kind: KindDuration, Default: "720h"},
			{Key: "ticketSecretPath", Label: "Ключ пропусков", Kind: KindText},
			{Key: "saltPath", Label: "Соль отпечатков", Kind: KindText, Hint: "Тот же файл запекается в лаунчер. Смена соли обнулит все известные компьютеры."},
		},
	},
	{
		Key:   "access",
		Title: "Доступ к сборкам",
		Hint:  "Кого пускать в закрытые сборки. Без правил все сборки открыты каждому.",
		Fields: []Field{
			{Key: "publicObjects", Label: "Файлы сборок открыты всем", Kind: KindBool, Default: "false", Hint: "Включите, если раздачу проверяет кто-то другой. Иначе файлы закрытых сборок отдаются только допущенным."},
		},
		Collections: []Collection{
			{
				Key: "sources", Title: "Списки допущенных", Keyed: true,
				NameLabel: "Имя списка", NameHint: "Коротко и латиницей — на это имя ссылаются правила.",
				Hint: "Откуда брать, кому можно: файл рядом с сервером или ваш сайт.",
				Fields: []Field{
					{Key: "type", Label: "Откуда список", Kind: KindChoice, Options: accessSourceNames},
					{Key: "config", Label: "Настройки списка", VariantOf: "type", Variants: map[string][]Field{
						"file": {
							{Key: "path", Label: "Файл списка", Kind: KindText, Hint: "JSON с ником и UUID допущенных."},
						},
						"http": {
							{Key: "url", Label: "Адрес проверки", Kind: KindText, Hint: "Подставляются %username% и %uuid%."},
							{Key: "mode", Label: "Как отвечает", Kind: KindChoice, Default: "ask", Options: func() []string { return []string{"ask", "list"} }, Hint: "ask — спрашиваем про игрока, list — забираем список целиком."},
							{Key: "method", Label: "Метод запроса", Kind: KindChoice, Default: "GET", Options: func() []string { return []string{"GET", "POST"} }},
							{Key: "headers", Label: "Заголовки запроса", Kind: KindPairs, Hint: "Через запятую: Authorization=Bearer …"},
							{Key: "timeout", Label: "Ждать ответа", Kind: KindDuration, Default: "3s"},
							{Key: "cacheTTL", Label: "Держать ответ в кэше", Kind: KindDuration, Default: "30s"},
							{Key: "failOpen", Label: "Пускать, если сайт молчит", Kind: KindBool, Default: "false", Hint: "Иначе при недоступном сайте закрытые сборки закрыты для всех."},
						},
					}},
				},
			},
			{
				Key: "rules", Title: "Правила доступа", Hint: "Какие сборки закрыты и каким списком.",
				Fields: []Field{
					{Key: "builds", Label: "Сборки", Kind: KindList, Hint: "Имена через запятую, можно с маской: test-*."},
					{Key: "source", Label: "Список допущенных", Kind: KindText, Hint: "Имя из списков выше."},
					{Key: "visibility", Label: "Видимость", Kind: KindChoice, Default: "listed", Options: func() []string { return []string{"listed", "hidden"} }, Hint: "listed — видна с замком, hidden — не видна вовсе."},
					{Key: "message", Label: "Что скажем недопущенному", Kind: KindText, Hint: "Например: напишите в Discord за доступом."},
				},
			},
		},
	},
	{
		Key:   "rateLimit",
		Title: "Защита от перебора",
		Hint:  "Счётчики тратит только неудачный вход, поэтому игроки с верным паролем их не видят.",
		Fields: []Field{
			{Key: "disabled", Label: "Выключить защиту", Kind: KindBool, Default: "false"},
			{Key: "backend", Label: "Где считать", Kind: KindChoice, Default: "memory", Options: func() []string { return []string{"memory", "redis"} }},
			{Key: "redisAddr", Label: "Адрес Redis", Kind: KindText, Default: "127.0.0.1:6379"},
			{Key: "login.limit", Label: "Попыток с адреса", Kind: KindInt, Default: "10"},
			{Key: "login.per", Label: "За время", Kind: KindDuration, Default: "1m"},
			{Key: "account.limit", Label: "Попыток на аккаунт", Kind: KindInt, Default: "30", Hint: "Тесный лимит позволил бы закрыть вход названному игроку."},
			{Key: "account.per", Label: "За время", Kind: KindDuration, Default: "10m"},
			{Key: "challenge.limit", Label: "Заданий на подпись", Kind: KindInt, Default: "30"},
			{Key: "challenge.per", Label: "За время", Kind: KindDuration, Default: "1m"},
		},
	},
	{
		Key:   "news",
		Title: "Новости в лаунчере",
		Hint:  "Простой текст: лаунчер держит сессию игрока, поэтому разметку извне он не показывает.",
		Fields: []Field{
			{Key: "source.type", Label: "Откуда брать", Kind: KindChoice, Options: news.SourceNames, Hint: "Пусто — новостей нет."},
			{Key: "limit", Label: "Сколько показывать", Kind: KindInt, Default: "10"},
			{Key: "source.config", Label: "Настройки источника", VariantOf: "source.type", Variants: map[string][]Field{
				"file": {
					{Key: "path", Label: "Файл новостей", Kind: KindText},
				},
				"http": {
					{Key: "url", Label: "Адрес новостей", Kind: KindText},
					{Key: "timeout", Label: "Ждать ответа", Kind: KindDuration, Default: "5s"},
					{Key: "cacheTTL", Label: "Держать в кэше", Kind: KindDuration, Default: "1m"},
					{Key: "headers", Label: "Заголовки запроса", Kind: KindPairs, Hint: "Через запятую: Authorization=Bearer …"},
				},
			}},
		},
	},
	{
		Key:   "branding",
		Title: "Оформление лаунчера",
		Hint:  "Как лаунчер выглядит у игрока. Картинки запекаются внутрь при сборке.",
		Fields: []Field{
			{Key: "name", Label: "Название проекта", Kind: KindText, Default: "Laminara"},
			{Key: "windowTitle", Label: "Заголовок окна", Kind: KindText},
			{Key: "tagline", Label: "Строка под названием", Kind: KindText, Hint: "Пусто — строки нет."},
			{Key: "primaryColor", Label: "Главный цвет", Kind: KindText, Default: "#ecc275"},
			{Key: "primaryInk", Label: "Цвет текста на нём", Kind: KindText, Default: "#241705"},
			{Key: "backgroundColor", Label: "Цвет фона", Kind: KindText, Default: "#0d0a09"},
			{Key: "logoPath", Label: "Логотип", Kind: KindText, Hint: "PNG или SVG рядом с сервером — уедет в лаунчер картинкой."},
			{Key: "heroMediaPath", Label: "Фон окна", Kind: KindText, Hint: "Картинка или видео. Видео тяжелее и заметно греет слабые машины."},
			{Key: "siteUrl", Label: "Сайт проекта", Kind: KindText},
		},
	},
	{
		Key:   "launcher",
		Title: "Обновления лаунчера",
		Hint:  "Папка с собранными лаунчерами: подпапка на версию, внутри — файлы под каждую систему.",
		Fields: []Field{
			{Key: "dir", Label: "Папка лаунчеров", Kind: KindText},
		},
	},
	{
		Key:   "modules",
		Title: "Модули",
		Hint:  "Свои дополнения к серверу: отдельные программы, которые добавляют команды и слушают события.",
		Fields: []Field{
			{Key: "dir", Label: "Папка модулей", Kind: KindText},
		},
		Collections: []Collection{
			{
				Key: "config", Title: "Настройки модулей", Keyed: true,
				NameLabel: "Имя модуля", NameHint: "Так же, как называется файл модуля в папке.",
				Hint: "Каждый модуль читает свои настройки — что в них писать, знает его автор.",
				Fields: []Field{
					{Key: "", Label: "Настройки модуля", Kind: KindText, Hint: "JSON целиком, например {\"token\":\"…\"}."},
				},
			},
		},
	},
}

func Sections() []Section { return schema }

func sectionOf(key string) (Section, bool) {
	for _, section := range schema {
		if section.Key == key {
			return section, true
		}
	}
	return Section{}, false
}

func collectionOf(section Section, key string) (Collection, bool) {
	for _, collection := range section.Collections {
		if collection.Key == key {
			return collection, true
		}
	}
	return Collection{}, false
}

func fieldIn(fields []Field, key string) (Field, bool) {
	for _, field := range fields {
		if field.Key == key {
			return field, true
		}
		if field.Variants == nil {
			continue
		}
		for _, set := range field.Variants {
			for _, nested := range set {
				if field.Key+"."+nested.Key == key {
					return nested, true
				}
			}
		}
	}
	return Field{}, false
}

func fieldOf(section Section, key string) (Field, bool) {
	return fieldIn(section.Fields, key)
}

func (f Field) options() []string {
	if f.Options == nil {
		return nil
	}
	return f.Options()
}

func accessSourceNames() []string { return access.SourceNames() }
