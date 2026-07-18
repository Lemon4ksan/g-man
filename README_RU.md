<div align="center">

<img src="assets/logo.png" alt="G-MAN Logo"/>

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/g-man)
[![Coverage](https://img.shields.io/badge/coverage-94.5%25-brightgreen?style=flat-square&logo=go&logoColor=white)](https://github.com/lemon4ksan/g-man)
[![License](https://img.shields.io/github/license/lemon4ksan/g-man?style=flat-square)](LICENSE)
[![Linter](https://img.shields.io/badge/linter-golangci--lint-brightgreen?style=flat-square&logo=go)](https://github.com/golangci/golangci-lint)

> _"Правильный бот в неправильном месте может изменить весь рынок скинов."_

#### 🇺🇸 [English](README.md) • 🇷🇺 [Русский](README_RU.md)

</div>

**G-man** — это высокопроизводительный, промышленный SDK клиентской части Steam и фреймворк для автоматизации игровых операций на Go. Разработанный для высокочастотного трейдинга, масштабного управления инвентарем и отказоустойчивой сетевой работы, G-man объединяет сеть Steam и игровые координаторы (Game Coordinators) в единый потокобезопасный оркестратор. Он бесшовно сочетает протоколы **Socket (CM)**, **WebAPI** и **игровые координаторы**, обеспечивая непрерывную работу ваших автоматизированных цепочек в режиме 24/7.

```shell
go get github.com/lemon4ksan/g-man
```

## ⚡ Ключевые возможности

* **Самовосстанавливающиеся сессии (Silent Re-auth):** Отслеживает состояние веб-сессий и токенов доступа в фоновом режиме. Если веб-куки истекают, оркестратор приостанавливает активные запросы, выполняет обновление OAuth2, обновляет хранилище и прозрачно возобновляет операции.
* **Двухстековый транспортный движок:** Динамически выбирает оптимальный путь: каналы TCP/WebSocket CM для синхронизации состояния в реальном времени или HTTPS WebAPI для массовых транзакций. Движок автоматически переходит на HTTP при разрыве сокета.
* **Конвейерная система сделок (Onion Trade Middleware):** Позволяет строить независимую логику проверки сделок. Обрабатывайте входящие предложения через цепочку фильтров: `Deduplicator` $\rightarrow$ `SecurityEscrowCheck` $\rightarrow$ `BlacklistFilter` $\rightarrow$ `PriceValidator` $\rightarrow$ `Verdict`.
* **Защитный веб-скрейпинг:** Анализирует HTML-страницы, возвращающие статус `200 OK`, на наличие скрытых «мягких ошибок» (например, сообщений о превышении лимитов, блокировках семейного просмотра или формах входа) и преобразует их в строго типизированные ошибки Go.
* **Надежное управление зависимостями:** Использует алгоритм поиска в глубину (DFS) для топологической сортировки и инициализации модулей при запуске, предотвращая циклические блокировки.

## ⚔️ Go против JavaScript: Почему G-man побеждает

В сфере автоматизации Steam исторически доминировал Node.js (библиотеки вроде `node-steam-user`). Однако при переходе на промышленный масштаб однопоточность JavaScript, накладные расходы рантайма V8 и динамическая природа языка становятся критическими узкими местами:

| Характеристика | 🤖 G-man (Go) | 📦 Node.js (JS/TS) | Почему это важно |
| :--- | :--- | :--- | :--- |
| **Модель многопоточности** | **CSP (Горутины + Каналы)** | Однопоточный Event Loop | Go эффективно распределяет тысячи сессий/аккаунтов по всем ядрам процессора без зависания шины событий. |
| **Потребление памяти (Idle)** | **~4.5 МБ** на клиента | **~40 - 80 МБ** на клиента | G-man позволяет запускать в 10–20 раз больше ботов на том же VPS, радикально снижая расходы на сервер. |
| **Сборка мусора (GC)** | **Низкая задержка (< 1мс)** | Склонна к микро-фризам (V8) | Исключает случайные задержки при обработке огромных инвентарей или во время пиковых торгов. |
| **Типизация** | **Строгая компиляционная** | Динамическая в рантайме (TS компилируется в JS) | Исключает критические ошибки вида `Cannot read property of undefined` во время проведения финансовых транзакций. |
| **Развертывание** | **Один статический файл (~15МБ)** | Тяжелая папка `node_modules` | Упрощает деплой и уменьшает размер Docker-образов с 300МБ+ до 15МБ (сборка на базе `scratch`). |

## 🔮 Будущее с Go: Почему это стандарт завтрашнего дня

Протоколы сети Steam постоянно усложняются. Взаимодействие с Game Coordinator (CS2, Dota 2) требует интенсивного использования Protobuf, а жесткие лимиты на частоту запросов (rate limits) делают необходимой продвинутую балансировку соединений. Go предоставляет надежный фундамент для долговечной автоматизации:

1. **Cloud-Native экосистема:** Go — это язык Docker, Kubernetes и Prometheus. G-man из коробки готов к интеграции в облачные микросервисы с нативным экспортом метрик и мониторингом через Grafana.
2. **Устойчивость без внешних зависимостей:** Забудьте о предупреждениях `npm audit`, сломанных обновлениях вложенных NPM-пакетов и уязвимостях цепочки поставок. Скомпилированный бинарник G-man содержит всё необходимое.
3. **Сверхэффективный Protobuf:** Сеть Steam построена на Protobuf. В Go работа с бинарными пакетами реализована нативно и работает в разы быстрее, чем парсинг JSON/JS объектов.
4. **Нативные сетевые примитивы:** Ротация прокси, кастомное DNS-разрешение и управление TCP/UDP сокетами реализуются просто с помощью стандартной библиотеки Go `net`, без необходимости в раздутых сторонних обертках.

## 🚀 Быстрый старт

### 1. Инициализация и запуск клиента

```go
package main

import (
	"context"
	"os"

	"github.com/lemon4ksan/miyako/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
	webtrading "github.com/lemon4ksan/g-man/pkg/trading/web"
)

func main() {
	// 1. Инициализируем хранилище сессий в JSON-файле
	store, _ := jsonfile.New("storage.json")
	logger := log.New(log.DefaultConfig(log.LevelInfo))

	// 2. Инициализируем оркестратор с необходимыми модулями
	client, _ := steam.NewClient(steam.DefaultConfig(),
		steam.WithLogger(logger),
		steam.WithStorage(store),
		webtrading.WithModule(webtrading.DefaultConfig()),
	)
	defer client.Close()

	if err := client.Run(); err != nil {
		panic(err)
	}

	// 3. Запрашиваем оптимальный сервер CM и выполняем вход
	dir := directory.New(client)
	server, _ := dir.GetOptimalCMServer(context.Background())
	login := auth.NewLogOnDetails(os.Getenv("STEAM_USER"), os.Getenv("STEAM_PASS"))

	if err := client.ConnectAndLogin(context.Background(), server, login); err != nil {
		panic(err)
	}

	client.Wait()
}
```

### 2. Конфигурация промежуточного ПО сделок (Onion Trade Middlewares)

```go
package main

import (
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

func PriceValidationMiddleware(priceLimit int) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			totalGiveValue := 0
			for _, item := range ctx.Offer.ItemsToGive {
				if price, ok := ctx.Get("price_" + item.SKU); ok {
					totalGiveValue += price.(int)
				}
			}

			if totalGiveValue > priceLimit {
				ctx.Review(reason.ReviewEngineError)
				return nil // Безопасно прерываем цепочку
			}

			return next(ctx)
		}
	}
}
```

## 📦 Экосистема и расширения

Архитектура G-man построена на модульности. Дополнительные инструменты и игровые пакеты вынесены в отдельные репозитории:

* **[g-man-cli](https://github.com/lemon4ksan/g-man-cli)**: Системный демон (`g-mand`) и консольный клиент (`gmanctl`) для управления, мониторинга и работы с инвентарями по протоколу gRPC.
* **[node-gman](https://github.com/lemon4ksan/node-gman)**: SDK на TypeScript/Node.js для демона `g-mand`. Предоставляет удобные адаптеры (`SteamUserAdapter`, `TradeOfferManagerAdapter`), упрощая миграцию или интеграцию существующего Node.js-кода с высокопроизводительным Go-бэкендом.
* **[g-man-tf2](https://github.com/lemon4ksan/g-man-tf2)**: Пакет автоматизации торговли и экономики Team Fortress 2.
  - **Металлическая арифметика:** Расчеты с ключами, очищенными, восстановленными металлами и ломом.
  - **Автоплавка:** Динамическое объединение оружия и плавка металлов для формирования точной сдачи.
  - **Синхронизация с Backpack.tf:** Управление листингами и демпинг цен конкурентов.

## 🚀 Производительность и эффективность памяти

* **Небольшой объем кучи:** Базовой архитектуре бота требуется приблизительно **~4.5 МБ** активной памяти кучи в режиме ожидания (включая шину событий, отслеживание друзей и менеджеры торговли).
* **Пулы буферов:** Сериализация сетевых пакетов использует пулы буферов для снижения нагрузки на GC при высокой интенсивности запросов.

## 🏗 Дорожная карта

- [x] **Маршрутизация транспорта:** Потокобезопасная отправка запросов через сокеты или HTTP.
- [x] **Сессии WebSession:** Фоновый keep-alive для веб-кук и API-ключей.
- [x] **Тихая повторная авторизация:** Фоновое восстановление просроченных JWT.
- [x] **Топологическая сортировка:** Детерминированный запуск модулей без циклов.
- [ ] **Загрузчик Steam CDN:** Динамическое скачивание манифестов и игровых ассетов.
- [ ] **Координатор CS2:** GC-рукопожатие, парсинг скинов и лобби-менеджер.
- [ ] **Координатор Dota 2:** Поддержка пакетов SOCache и управление лобби.

## 🤝 Участие в разработке

Мы рады новым участникам! Перед тем как внести свой вклад в проект обязательно:

1. Ознакомьтесь с философией проектирования в [CONTRIBUTING.md](CONTRIBUTING.md).
2. Минимизируйте сетевые зависимости, пропуская трафик через интерфейс `transport.Doer` для удобства тестирования.
3. Проверяйте изменения на потокобезопасность с помощью `go test -race ./...`.

## ☕ Поддержка проекта

Создание масштабируемых и отказоустойчивых ботов для Steam требует сотен часов обратной разработки протоколов. Если G-MAN помог сэкономить ресурсы ваших серверов или автоматизировать ваши торговые операции, вы можете поддержать проект:

<div align="center">

[![Trade Offer](https://img.shields.io/badge/Steam-Trade_Offer-blue?style=for-the-badge&logo=steam)](https://steamcommunity.com/tradeoffer/new/?partner=1141078357&token=HjsTJQFX)

> _"Пожертвования... не обязательны, но... соответствуют условиям нашего... соглашения."_

</div>

## ⚖️ Лицензия и правовая информация

**Дисклеймер:** Это программное обеспечение **не** связано с корпорацией **Valve**, не поддерживается и не одобряется ей. Steam, Team Fortress 2 и все сопутствующие товарные знаки являются собственностью Valve Corporation. Вы используете данный фреймворк на свой страх и риск.

Проект распространяется под лицензией **BSD 3-Clause License**. Подробности в файле [LICENSE](LICENSE).

---

<div align="center">
  <sub>Prepare for unforeseen consequences... or just prepare for the next Steam Sale.</sub>
</div>
