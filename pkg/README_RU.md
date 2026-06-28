<div align="center">

# 📦 G-MAN SDK Пакеты

### Модульные, интерфейсно-ориентированные компоненты для автоматизации Steam и игровых координаторов

#### 🇺🇸 [English](README.md) • 🇷🇺 [Русский](README_RU.md)

</div>

Этот каталог содержит общедоступные пакеты G-man. Вы можете импортировать фреймворк целиком или точечно выбирать отдельные пакеты (например, `steam/community` для скрейпинга, `trading/engine` для создания конвейеров проверок или `crypto` для генерации кодов TOTP) для интеграции в существующие проекты.

## 🏗 Иерархия зависимостей пакетов

Чтобы поддерживать оптимальную структуру и избегать циклического импорта, G-man строго следует **многоуровневой иерархии импортов**. Нижние слои никогда не должны импортировать пакеты из верхних:

```mermaid
flowchart TD
    classDef l4_node fill:#24273a,stroke:#cba6f7,stroke-width:2px,color:#cdd6f4,rx:8,ry:8;
    classDef l3_node fill:#1e1e2e,stroke:#89b4fa,stroke-width:2px,color:#cdd6f4,rx:8,ry:8;
    classDef l2_node fill:#181825,stroke:#a6e3a1,stroke-width:2px,color:#cdd6f4,rx:8,ry:8;
    classDef l1_node fill:#11111b,stroke:#f38ba8,stroke-width:2px,color:#cdd6f4,rx:8,ry:8;

    subgraph L4 ["🚀 Слой 4: Домен и исполнение (Бизнес-логика)"]
        direction LR
        Trading["<b>🤝 trading</b><br/>Движок Middleware<br/>Обработка предложений"]
        Behavior["<b>🤖 behavior</b><br/>Автономные сценарии<br/>Достижения, Сессии и Guard"]
        Client["<b>🤖 steam.Client</b><br/>Центральный оркестратор<br/>Управление жизненным циклом"]
        
        Trading ~~~ Behavior ~~~ Client
    end
    class L4,Trading,Behavior,Client l4_node;

    subgraph L3 ["🌐 Слой 3: Сетевой транспорт Steam и социальный слой"]
        direction LR
        Socket["<b>🔌 steam/socket</b><br/>Потоковый CM TCP/WSS<br/>Диспетчер EMsgs"]
        Auth["<b>🔐 steam/auth</b><br/>OAuth2 & Токены<br/>Беззвучная переавторизация"]
        Comm["<b>🛡️ steam/community</b><br/>Защищенные скрейперы<br/>Маркет и инвентарь"]
        Social["<b>💬 steam/social</b><br/>Друзья, чат<br/>Состояния профиля"]
        
        Socket ~~~ Auth
        Auth ~~~ Comm
        Comm ~~~ Social
    end
    class L3,Socket,Auth,Comm,Social l3_node;

    subgraph L2 ["📜 Слой 2: Базовый слой Steam и сериализация"]
        direction LR
        Protocol["<b>📦 steam/protocol & protobuf</b><br/>Компилируемые Protobufs<br/>Парсер VDF и EMsgs"]
        Encoding["<b>⚙️ steam/encoding</b><br/>Парсеры VDF и BVDF<br/>Бинарные данные"]
        SID["<b>🆔 steam/id</b><br/>Формат SteamID<br/>Типы аккаунтов"]
        
        Protocol ~~~ Encoding
        Encoding ~~~ SID
    end
    class L2,Protocol,Encoding,SID l2_node;

    subgraph L1 ["🛠️ Слой 1: Инфраструктурные утилиты"]
        direction LR
        Log["<b>📊 log</b><br/>Структурированный логгер"]
        Net["<b>🌐 network</b><br/>Базовый TCP/WS транспорт"]
        Storage["<b>💾 storage</b><br/>Хранение состояния (JSON/Память)"]
        Crypto["<b>🔑 crypto</b><br/>RSA/AES и Steam TOTP"]
        Bus["<b>🚌 miyako/bus</b><br/>Потокобезопасный Pub/Sub"]
        
        Log ~~~ Net
        Net ~~~ Storage
        Storage ~~~ Crypto
        Crypto ~~~ Bus
    end
    class L1,Log,Net,Storage,Crypto,Bus l1_node;

    L4 ==>|Использует сервисы| L3
    L3 ==>|Сериализуется через| L2
    L2 ==>|Зависит от| L1

    style L4 fill:#2b1836,stroke:#cba6f7,stroke-width:2px,stroke-dasharray: 5 5,color:#cba6f7
    style L3 fill:#1a2235,stroke:#89b4fa,stroke-width:2px,stroke-dasharray: 5 5,color:#89b4fa
    style L2 fill:#182823,stroke:#a6e3a1,stroke-width:2px,stroke-dasharray: 5 5,color:#a6e3a1
    style L1 fill:#301820,stroke:#f38ba8,stroke-width:2px,stroke-dasharray: 5 5,color:#f38ba8
```

## 📦 Обзор пакетов

### 1. Базовый слой и Protobuf (`pkg/steam` & `pkg/protobuf`)
Фундамент фреймворка, реализующий сетевое взаимодействие, сериализацию протоколов и оркестрацию API.

| Пакет | Описание |
| :--- | :--- |
| **[steam](steam/)** | Главный Orchestrator. Связывает сокеты, авторизацию и доменные модули в единый потокобезопасный клиент. |
| **[steam/auth](steam/auth/)** | Сценарии авторизации OAuth2, отслеживание JWT и фоновое обновление сессии. |
| **[steam/community](steam/community/)** | Защищенный веб-клиент для работы с инвентарями `steamcommunity.com`, торговой площадкой и авторизацией OpenID. |
| **[steam/encoding](steam/encoding/)** | Сериализация KeyValues (VDF), кодирование и декодирование Binary VDF (BVDF). |
| **[steam/guard](steam/guard/)** | Подтверждение операций в мобильном аутентификаторе Steam Guard, генерация кодов 2FA и управление сессией. |
| **[steam/id](steam/id/)** | Парсер и форматирование идентификаторов `SteamID` (поддержка SID2, SID3 и 64-битных значений). |
| **[steam/socket](steam/socket/)** | Стейтфул-клиент для CM-сокетов, управляющий пингами, маршрутизацией и асинхронными задачами. |
| **[steam/service](steam/service/)** | Коммандер RPC, транслирующий Protobuf-сообщения в унифицированные сервисные вызовы. |
| **[steam/social](steam/social/)** | Социальные функции: статусы пользователей в реальном времени, списки друзей и чат. |
| **[steam/transport](steam/transport/)** | Двухстековый транспортный мост, объединяющий CM-сокеты и HTTP в единую абстракцию. |
| **[steam/webapi](steam/webapi/)** | Автоматически сгенерированные обертки для официальных Web API Steam. |
| **[protobuf](protobuf/)** | Сгенерированные спецификации Protobuf Steam (`steam`) и пользовательские структуры (`custom`). |

### 2. Системные и игровые координаторы (`pkg/steam/sys`)
Шлюзы к внутренним механизмам Steam и серверам конкретных игр.

| Пакет | Описание |
| :--- | :--- |
| **[sys/account](steam/sys/account/)** | Управление статусом безопасности аккаунта и системными параметрами. |
| **[sys/apps](steam/sys/apps/)** | Управление статусом нахождения в игре и обработка сокет-уведомлений приложений. |
| **[sys/directory](steam/sys/directory/)** | Клиент API ISteamDirectory для динамического получения списков активных IP-адресов CM-серверов. |
| **[sys/gc](steam/sys/gc/)** | Базовый клиент игрового координатора (Game Coordinator). Управление рукопожатиями и мультиплексированием пакетов. |
| **[sys/notifications](steam/sys/notifications/)** | Подсистема получения и обработки платформенных пуш-уведомлений Steam. |

### 3. Торговая логика (`pkg/trading`)
Высокоуровневый движок обработки запросов торговых предложений.

| Пакет | Описание |
| :--- | :--- |
| **[trading/engine](trading/engine/)** | Движок **Onion Middleware**. Строит конвейер проверок сделки с передачей контекста. |
| **[trading/processor](trading/processor/)** | Менеджер жизненного цикла транзакции (*Проверка $\rightarrow$ Решение $\rightarrow$ Действие $\rightarrow$ Уведомление*). |
| **[trading/reason](trading/reason/)** | Причины аудита сделок, структурированные коды ошибок и типы вердиктов. |
| **[trading/notifications](trading/notifications/)** | Асинхронные события торговли и вещание обновлений статуса обменов. |
| **[trading/review](trading/review/)** | Аудит ценных транзакций, логирование сделок и административный разбор. |
| **[trading/live](trading/live/)** | Поддержка игровых сессий обмена в реальном времени ("Live Trade") через GC. |
| **[trading/web](trading/web/)** | Веб-операции обмена предложениями через API сообщества и их обработка. |

### 4. Инфраструктура и служебные пакеты
Вспомогательные утилиты, используемые в рамках всего SDK.

| Пакет | Описание |
| :--- | :--- |
| **[behavior](behavior/)** | Сценарии автоматического поведения ботов (эмуляция достижений, проверка сессий, авто-принятие подтверждений Guard). |
| **[command](command/)** | Регистрация CLI-команд, валидация типов на основе рефлексии и их выполнение. |
| **[crypto](crypto/)** | Алгоритмы RSA/AES, генерация TOTP и подписи для мобильной авторизации Steam. |
| **[log](log/)** | Контекстный асинхронный структурированный логгер с поддержкой Correlation ID. |
| **[network](network/)** | Базовый сетевой слой TCP и WebSocket сокетов, фреймеры сообщений и унифицированные ошибки. |
| **[storage](storage/)** | Провайдер постоянного хранения данных с адаптерами JSON (`jsonfile`) и памяти (`memory`). |

## 📐 Архитектура и принципы проектирования

Пакеты G-man спроектированы в соответствии с ключевыми практиками языка Go:

1. **Изолированное тестирование (Mockability):** Структуры зависят от лаконичных интерфейсов (таких как `transport.Doer` или `storage.Provider`), а не от конкретных реализаций, что упрощает написание модульных тестов.
2. **Конкурентность на базе каналов:** Передача событий внутри системы осуществляется через шину событий `miyako/bus` во избежание взаимных блокировок. Совместно используемое состояние защищено при помощи `sync/atomic` и RWMutex.
3. **Декаплинг расширений:** Чтобы избежать раздувания кодовой базы, специализированная логика игровых экономик вынесена во внешние пакеты (например, `g-man-tf2`). Это позволяет ядру оставаться легким и производительным.
