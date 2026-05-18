<div align="center">

# ⚙️ Базовый оркестратор Steam

### «Мозг» G-man: унифицированный транспорт, авторизация и протоколы

#### 🇺🇸 [English](README.md) • 🇷🇺 [Русский](README_RU.md)

</div>

Пакет `steam` является фундаментальным оркестрирующим ядром фреймворка G-man. Он берет на себя всю сложность низкоуровневой инфраструктуры Valve, объединяя три независимых окружения в единый потокобезопасный клиент `steam.Client`:

1. **Постоянные CM-соединения**: Низкозадержечный бинарный обмен данными через Connection Manager сокеты Steam (TCP/WebSockets).
2. **Сервисы WebAPI**: Стандартизированные удаленные вызовы (RPC) по протоколу HTTP (`api.steampowered.com`).
3. **Сообщество Steam (Community)**: Защищенный скрейпинг и взаимодействие с сайтом `steamcommunity.com`.

## ⚡ Глубокий разбор ключевых систем

### 1. 🚦 Двухстековая маршрутизация запросов
G-man придерживается **принципа независимости от протокола**. Когда вы отправляете запрос через роутер `Service()`, вам не нужно вручную выбирать тип транспорта.

Роутер динамически оценивает тип вызова:
* Если бот подключен к сокету CM и запрос поддерживает формат `EMsg`, передача идет напрямую через **бинарный сокет** для достижения максимальной скорости.
* Если сокет временно недоступен или эндпоинт требует HTTP-вызова (например, классические методы WebAPI), роутер прозрачно отправляет запрос через **клиент HTTP WebAPI**.

### 2. 🔄 Автовосстановление сессий (Self-Healing OAuth2)
Система авторизации Valve состоит из множества слоев: Refresh-токены, JWT Access-токены и веб-куки.
Оркестратор `Client` полностью автоматизирует этот процесс в фоне:
* **Фоновый мониторинг**: Клиент постоянно отслеживает время жизни токенов.
* **Атомарное обновление**: Если во время выполнения запроса происходит истечение срока действия кук или токена (ошибка `401 Unauthorized`), `Client` ставит текущий запрос на паузу, выполняет безопасный фоновый OAuth2-рефреш, обновляет сессию и прозрачно возобновляет исходный запрос.
* **Незаметно для разработчика**: В вашей бизнес-логике вам никогда не придется обрабатывать ошибки авторизации или писать повторные попытки входа.

### 3. 📡 Событийная шина
Для взаимодействия между частями системы пакет `steam` использует потокобезопасную неблокирующую шину событий (`pkg/bus`). Любые внешние системы или модули могут легко подписаться на глобальные события Steam.

**Доступные ключевые события:**
* `LoggedOnEvent` / `DisconnectedEvent` — статус подключения к сети Valve.
* `SessionUpdatedEvent` — обновление токенов и сессионных кук.
* `IncomingPacketEvent` — перехват сырых пакетов сокета (`EMsg`).
* `WebAPIKeyRegisteredEvent` — автоматическая регистрация/получение ключа WebAPI.

## 🚀 Быстрый старт

Пример инициализации оркестратора, авторизации и выполнения удаленного RPC-вызова:

```go
package main

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	"google.golang.org/protobuf/proto"
)

func main() {
	ctx := context.Background()

	// 1. Инициализация структурированного логирования
	logger := log.New(log.DefaultConfig(log.LevelInfo))
	defer logger.Close()

	// 2. Инициализация оркестратора со стандартными настройками и логгером
	cfg := steam.DefaultConfig()
	client, err := steam.NewClient(cfg, steam.WithLogger(logger))
	if err != nil {
		logger.Error("Не удалось инициализировать клиент Steam", log.Err(err))
		return
	}
	defer func() {
		_ = client.Close()
		client.Wait()
	}()

	// 3. Подготовка данных для авторизации
	details := &auth.LogOnDetails{
		AccountName: "your_username",
		Password:    "your_password",
		// Опционально: TwoFactorCode или AuthCode
	}

	// 4. Получение оптимального сервера Connection Manager (CM) через Directory Service
	dir := directory.New(client.Service())
	server, err := dir.GetOptimalCMServer(ctx)
	if err != nil {
		logger.Error("Не удалось получить оптимальный сервер CM", log.Err(err))
		return
	}

	// 5. Подключение к серверам Steam и авторизация
	logger.Info("Подключение к сети Steam CM...")
	if err := client.ConnectAndLogin(ctx, server, details); err != nil {
		logger.Error("Ошибка входа", log.Err(err))
		return
	}
	defer func() {
		_ = client.Disconnect()
	}()

	// 6. Выполнение Unified RPC-запроса через умный роутер
	req := &pb.CPlayer_GetNickname_Request{
		Steamid: proto.Uint64(76561198000000000), // Целевой SteamID
	}
	
	// Роутер Service() сам решит, отправить запрос по сокету или HTTP
	res, err := service.Unified[pb.CPlayer_GetNickname_Response](ctx, client.Service(), req)
	if err != nil {
		logger.Error("Ошибка вызова RPC", log.Err(err))
		return
	}

	logger.Info("Никнейм пользователя успешно получен", log.String("nickname", res.GetNickname()))
}
```

### 🔑 Ключевые архитектурные моменты

При создании полноценного продакшн-бота (например, как в [примере TF2-бота](file:///d:/CodingProjects/g-man/examples/tf2_bot/main.go)), используйте встроенные возможности архитектуры G-man:

1. **Внедрение модулей (Functional Options)**: Необходимые модули (такие как TF2, Backpack, Schema) должны передаваться при инициализации клиента с помощью функциональных опций вместо ручной регистрации:
   ```go
   client, err := steam.NewClient(cfg,
       steam.WithLogger(logger),
       tf2.WithModule(),
       backpack.WithModule(),
       webtrading.WithModule(webtrading.Config{PollInterval: 30 * time.Second}),
   )
   ```

2. **Событийно-ориентированная логика**: Избегайте синхронных блокировок и используйте встроенную потокобезопасную шину событий клиента G-man для обработки входа, подтверждений Steam Guard и смены состояний сессии:
   ```go
   sub := client.Bus().Subscribe(&auth.LoggedOnEvent{}, &auth.SteamGuardRequiredEvent{})
   go func() {
       for event := range sub.C() {
           switch ev := event.(type) {
           case *auth.LoggedOnEvent:
               logger.Info("Успешный вход в сеть!", log.Uint64("steam_id", ev.SteamID))
           }
       }
   }()
   ```

3. **Интеграция конвейера обработки трейдов (Middleware)**: Создавайте гибкие, многослойные цепочки проверок предложений обмена (onion-style middleware), динамически связывая внешние модули, менеджеры валют и ценовые базы данных.

