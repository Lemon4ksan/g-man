<div align="center">

# 🎒 Доменный движок Team Fortress 2

### Промышленная автоматизация экономики, ценообразования и интеграции с GC

#### 🇺🇸 [English](README.md) • 🇷🇺 [Русский](README_RU.md)

</div>

## ⚡ Разбор ключевых возможностей

### 🪙 1. Безопасная арифметика валюты (`tf2/currency`)
Торговля в TF2 требует ювелирных расчетов в **Ключах** и **Металле (Scrap, Reclaimed, Refined)**. Стандартная математика с использованием `float64` приводит к накоплению погрешностей округления (например, `0.11 + 0.22 = 0.33000000000000007`), что фатально для автоматических ботов (приводит к отмене сделок или неверной выдаче сдачи).

Пакет `currency` решает эту проблему за счет представления дробных долей металла в виде целочисленных единиц базового лома (`Scrap`):
* **Пример**: Сложение `1.55 ref` и `0.55 ref` гарантированно дает `2.11 ref`. Он также автоматически и безопасно конвертирует металлы в ключи и обратно на основе живых курсов из PriceDB.

### 🎒 2. Синхронизация с Игровым Координатором через `SOCache` (`tf2/backpack`)
Обычные боты запрашивают инвентарь через веб-интерфейс Steam API (HTTP), что сопряжено с жесткими лимитами запросов (rate limits) и задержками обновления данных.
G-man поддерживает постоянный синхронный **SOCache (Shared Object Cache)** непосредственно в памяти игрового координатора:
* При крафте, обмене или удалении предмета Valve присылает бинарное дельта-обновление по сокету. Клиент `tf2.Client` мгновенно применяет патч к локальному кэшу памяти и генерирует событие `BackpackLoadedEvent`, обеспечивая нулевую задержку доступа к реальному инвентарю бота.

### 📈 3. Локальный кэш цен PriceDB и автопрайсинг (`tf2/bptf` & `tf2/pricedb`)
Чтобы бот не отправлял лишние HTTP-запросы при оценке каждого трейда, G-man использует потокобезопасное хранилище цен `PriceDB` в памяти.
* Пакет `bptf` подключается по протоколу WebSockets (`Socket.IO`) напрямую к потоку обновлений цен от `backpack.tf`.
* При изменении цены на рынке локальный кэш цен обновляется мгновенно в фоне, делая новые рейты доступными для конвейера валидации `trading/engine`.

## 🚀 Быстрый старт

Пример запуска TF2-модуля, выполнения точных расчетов сдачи и перехвата событий обновления инвентаря:

```go
package main

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/backpack"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
)

func main() {
	ctx := context.Background()

	// 1. Инициализация структурированного логирования
	logger := log.New(log.DefaultConfig(log.LevelInfo))
	defer logger.Close()

	// 2. Инициализация клиента Steam с модулями TF2 и Backpack
	cfg := steam.DefaultConfig()
	steamClient, err := steam.NewClient(cfg,
		steam.WithLogger(logger),
		tf2.WithModule(),      // Регистрирует tf2.ModuleName
		backpack.WithModule(), // Регистрирует backpack.ModuleName
	)
	if err != nil {
		logger.Error("Не удалось инициализировать клиент Steam", log.Err(err))
		return
	}
	defer func() {
		_ = steamClient.Close()
		steamClient.Wait()
	}()

	// Получение ссылок на зарегистрированные модули
	tf2Mod := steamClient.Module(tf2.ModuleName).(*tf2.TF2)
	bpMod := steamClient.Module(backpack.ModuleName).(*backpack.Backpack)

	// 3. Демонстрация безопасных валютных вычислений
	// 1.55 ref + 0.55 ref = 2.10 ref (14 scrap + 5 scrap = 19 scrap = 2.11 ref)
	// G-man обеспечивает точное сложение float64 через конвертацию в Scrap в фоне:
	totalRef := currency.AddRefined(1.55, 0.55)
	logger.Info("Безопасное сложение очищенного металла выполнено", log.Float64("total_ref", totalRef)) // Выведет: 2.11

	// 4. Подписка на реалтайм-обновления инвентаря из GC SOCache
	sub := steamClient.Bus().Subscribe(&tf2.BackpackLoadedEvent{})
	go func() {
		for event := range sub.C() {
			if bpEvent, ok := event.(*tf2.BackpackLoadedEvent); ok {
				logger.Info("Инвентарь TF2 мгновенно синхронизирован через SOCache!", log.Int("items_count", bpEvent.Count))
				
				// Доступ к чистой валюте в инвентаре
				pure := bpMod.GetPureStock()
				logger.Info("Доступный баланс валюты",
					log.Int("keys", pure.Keys),
					log.Float64("refined", pure.TotalRefined()),
				)
			}
		}
	}()

	// Блокировка главного потока (или ожидание системных сигналов завершения)
	select {}
}
```

### 🔑 Ключевые архитектурные моменты

При построении продакшн-бота (например, как в [примере торгового бота](/examples/tf2_bot/main.go)), используйте встроенные возможности пакета `tf2`:

1. **Синхронизация цен через PriceDB**: Избегайте постоянных HTTP-запросов. Используйте WebSocket-клиент для получения цен в реальном времени от `backpack.tf` и сохраняйте их в локальном менеджере `pricedb.Manager`:
   ```go
   pdbClient := pricedb.NewClient(httpClient)
   pdbManager := pricedb.NewManager(pdbClient, logger)
   ```

2. **Автоматический крафт и сдача (Metal Management)**: Используйте менеджер крафта для автоматической переплавки дубликатов оружия и размена металла при нехватке чистой валюты во время сделки:
   ```go
   craftingManager := crafting.NewManager(bp, tf2Mod)
   metalManager := crafting.NewMetalManager(bp, craftingManager, logger)
   ```

3. **Конвейер проверок (Trade Middlewares)**: Объединяйте лимиты запасов (stock limits), проверку банов, оценку стоимости и механизм умного контр-предложения в единую цепочку промежуточного ПО:
   ```go
   tradeEngine.Use(
       tf2trading.EscrowMiddleware(webTradeManager, logger),
       tf2trading.PricerMiddleware(pdbManager, logger),
       tf2trading.StockLimitMiddleware(bp, stockCfg, logger),
       tf2trading.SmartCounterMiddleware(metalManager, bp, webTradeManager, logger),
   )
   ```

## 📚 Рекомендации разработчику

* **Всегда используйте SKU (`tf2/sku`) для идентификации предметов**: Никогда не сравнивайте предметы только по `defindex`. Такие свойства, как качество, краска, серии убийств (killstreaks) кардинально меняют цену предмета. Всегда преобразовывайте структуру `tf2.Item` в строковый SKU (например, `5021;6` для ключа Манн Ко) перед сохранением в базу или поиском цен.
* **Используйте SOCache вместо WebAPI**: При написании торговой логики запрашивайте инвентарь через `tf2.Client.Backpack()`, а не через веб-скрейпер `community`, чтобы гарантировать проведение сделок с реальным состоянием инвентаря на стороне Valve.
