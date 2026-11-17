# Rust-ядро — текущее состояние

Спека: `SPEC.md`. План M0: `plans/M0-foundation.md`. План M1: `plans/M1-api.md`. Промпты: `prompts/`.
Ledger исполнения (git-ignored): `.superpowers/sdd/M0-foundation/progress.md`.

## Сделано (M0 закрыт)
- Workspace: `[workspace.lints] unsafe_code = "forbid"`, общие зависимости.
- Генератор эталонов `tools/rust-fixtures` (Go) → `crates/fixtures/m0/` (29 JSON-эталонов: varint, meta_gencode,
  wrapper, api_record, gojson, dsd, query, sqlite_records, gostrconv, config, endpoints, geoip, intel, jess,
  jess-index, nameserver, netquery, netstate, network, notifications, process, profile, resolver, rng, status,
  updates и др. — полный актуальный список в каталоге `crates/fixtures/m0/`).
  Режим проверки: `go run ./tools/rust-fixtures -verify <location>` открывает БД, созданную Rust, и печатает строки и миграции.
- `pm-dsd` — форматы, varint, gzip, dsdl, Go-JSON.
- `pm-record` — Key/Meta/Record, `_meta`-JSON, accessor.
- `pm-query` — парсер, условия, `Print`, `Matches`; 46 кейсов `query.json` + порт Go-тестов.
- `pm-database` — реестр, контроллер, хуки, подписки, sqlite (файл совместим с Go), injected/memory.
- `pm-config` — опции, слои значений, config.json байт-в-байт как Go, перспективы, injected DB `config`.
- `pm-mgr` — влит в основную ветку.

## В работе
- M3: `pm-updates` — модель индекса обновлений (`Artifact`/`Index`: JSON, платформа `GOOS_GOARCH`, фильтрация
  по платформе, валидация имён файлов против path traversal, версии с наследованием от индекса),
  `CanDoUpgrades`/`ShouldUpgradeTo` (включая zero-version bootstrap, breaking downgrade, CDN-защиту,
  `ErrSameIndex`, локально сгенерированные индексы), SHA-256 проверки файлов/данных, `VerifyArtifacts`,
  лояльный semver как в `hashicorp/go-version` (v-префикс, паддинг сегментов, prerelease/metadata).
  Подпись индекса — через `pm-jess` (порт проверки и `sign`, проверено на реальном подписанном индексе реальными ключами). Загрузчик (`downloader.go`): HTTP(S)
  через трейт `HttpClient` (`HttpsClient` на ureq), gzip/zip-декомпрессия с лимитом 1 ГБ, скачивание/проверка
  артефактов с копированием из уже существующих файлов по SHA-256, запись `.download` + rename, права 0755/0644.
  Портированы также применение апгрейда (перемещение файлов, post-upgrade команды, рестарт), сканер каталога
  (`index_scan.go`), воркер/модуль апдейтера (`Updater`: проверка, уведомления, auto-download/auto-apply,
  forced update, событие `resource update`, `Config::Check`). Остатки: UI-lock апгрейда и рестарт по callback в ядре.
- M3: `pm-geoip` — MaxMind-базы (crate `maxminddb`, загрузка в память вместо mmap без `unsafe`), `Location`,
  `CountryInfo`/`ContinentInfo`, `Coordinates`, `Region`, `AddCountryInfo` (anycast → `__`, центр страны),
  `EstimateNetworkProximity` (страна/регион/сосед/ASN/ASOrg + haversine, штраф за точность), `PrimitiveNetworkProximity`,
  `ASOrgUnknown`, `GetCountryInfo`. Таблицы стран (248) и регионов (23) сгенерированы из Go-исходников
  (`go/ast`) в `crates/pm-geoip/data/geoip-data.json` и **сверены с рантаймом Go** (`GetCountryInfo`,
  `IsRegionalNeighbor`) при генерации. Подключён в `pm-core`. Есть загрузка по имени ресурса обновления
  (`geoipv4.mmdb`/`geoipv6.mmdb`). Воркер обновлений (перезагрузка баз из intel-индекса по событию ресурса) и API-эндпоинт `intel/geoip/countries` портированы.
- M3: `pm-intel` — filterlists: bloom-фильтры, **байт-в-байт** совместимые с Go `tannerryan/ring`
  (MurmurHash3 x64 128, 4-хеш схема, сериализация v1; размеры/раунды сверены), DSDL-декодер (заголовок,
  gzip, varint + JSON-записи), нормализация доменов, записи `cache:intel/filterlists/*`, поиск
  country/domain/asn/ipv4/ipv6, кэш bloom-фильтров и версия кэша (`Reset`). Подключён в `pm-core` (БД `cache`).
  Плюс `block_reason.go`: `ListMatch`/`ListBlockReason` с Go-форматированием и JSON-обёрткой `filterlists`
  (пустые списки — `null`, как nil-срезы в Go) и `entity.go`: `Entity` (IP/домен/CNAME, scope, GeoIP-локация,
  загрузка списков домена/ASN/страны/IP, `MatchLists`, `ListBlockReason`, reverse-resolve через колбэк,
  сброс и флаги sub-domain/CNAME). `splitDomain` — на крейте `psl`, результаты сверены с Go на 21 домене
  (wildcard `*.ck`, exception `!www.ck`, private `appspot.com`, неизвестный TLD, IP). Локация — через трейт
  `LocationProvider` (в тестах — статическая карта). Портированы: индекс фильтр-листов (`index.dsd`, категории/источники/unbreak), updater (`service/updates`), `customlists` и модуль-lifecycle (`FilterListsModule`, `CustomListService`);
  TXT-записи причин блокировки (`nsutil.RRProvider`) — через `pm-nameserver` (A3).
- M2: `pm-status` — общий статус системы: состояния модулей (`mgr.State`/`StateUpdate` из `pm-mgr`),
  публикация `runtime:system/status` (провайдер считает запись на каждый запрос, `PushFunc` — подписчикам),
  уведомления из состояний (warning/error → на системe + «Get Help», hint/undefined → info без действий,
  удаление уведомлений исчезнувших состояний), worst state по severity, миграция security level → bool.
  JSON сверен с Go (`status.json`), включая «плоский» `WorstState` (Go встраивает `mgr.State`).
- M2: `pm-netenv` — минимальный порт: `OnlineStatus` (имена и числа как в Go), `CaptivePortal`
  (домен/IP из URL, спец-домен для IP), `NetEnv` с атомарным статусом, порталом и событиями изменений,
  `global()`/`shared()`. Активные проверки (DNS/HTTP-пробы, интерфейсы, ICMP, отслеживание смены сети) не портированы.
- M2: `pm-runtime` — реестр провайдеров значений (ключ/префикс, самый длинный префикс), read-only/write-only
  провайдеры, `SingleRecord` (точный ключ), трассировка, `Get`/`Put`/`Query` с проверкой валидности и прав,
  injected-БД `runtime` и `PushFunc` для подписчиков. Подключён в `pm-core` (третья БД `runtime`).
- M2: `pm-notifications` — модель уведомления (JSON сверен с Go), типы/состояния/действия, вывод EventID
  (`unknown:<uuid v5 instance>`) и GUID, реестр с удалением/истечением/очисткой, выбор действия с Go-семантикой
  (`responded` без обработчика и без ожидающего, `executed` при обработчике или слушателе), injected-БД
  `notifications` (get/put/query/delete, ключи `all/<EventID>`), опция `core/useSystemNotifications`.
- M2: `pm-profile` — отпечатки (tag/cmdline/env/path × equals/prefix/regex) со скорингом как в Go,
  вывод ID профиля (SHA3-256 + base58, с го-квирком компакции), модель профиля (JSON сверен),
  слияние профилей, парсинг настроек профиля, хранилище `core:profiles/*`, поиск лучшего профиля с конфликтами.
  `LayeredProfile` пока без слияния глобальных слоёв; правила endpoints не парсятся (нужен `pm-endpoints`).
- M2: `pm-process` — кэш процессов (`pid-createdAt`), чтение `/proc` (stat/status/cmdline/environ/exe/cwd, btime→ms как в gopsutil),
  спец-процессы (unidentified/unsolicited/system), группы процессов, теги (trait), очистка, записи `network:tree/*` в БД,
  атрибуция соединений с фолбэками (localhost/ICMP/inbound). JSON процесса сверен с Go (`process.json`).
  Хэши исполняемых файлов портированы (`get_exec_hash`: md5/sha1/sha256, кэш в `ExecHashes`; неизвестный
  алгоритм — ошибка, а не паника Go); профили в процессе — нет.
- M2: `pm-netstate` — парсинг `/proc/net/{tcp,tcp6,udp,udp6}`, поиск PID по inode через `/proc/<pid>/fd`,
  кэши (PID по uid 10 мс, владельцы сокетов поколениями 1 с), таблицы TCP/UDP с dual-stack, `Lookup`/`Exists`,
  UDP-направления соединений. Фикстура `netstate.json` (санированная: адреса/порты/inode/PID заменены).
- M1: `pm-api` — HTTP-слой, WS DB API и сессии-куки готовы (реестр эндпоинтов, права, API-ключи, роутер,
  config/options, `/api/database/v1` с подписками, `Portmaster-API-Token`, axum).
- M1: `pm-core` — бинарник `portmaster-core-rs`: каталоги, БД `core`/`cache`, опции `core/devMode`,
  `core/log/level`, `core/listenAddress`, `core/apiKeys`, загрузка `config.json`, API и корректное завершение по SIGTERM.
  Раздача UI из zip работает (`/`, `/ui/modules/<module>/*`, `/assets/*`), проверено на реальном `portmaster-ui-2.2.3.zip`.
  A1: авторизация клиентов API по процессу (`api_auth.rs`, порт `firewall/api.go`, флаг `--allowed-clients`), без `SO_PEERCRED`. План: `plans/M1-api.md`.

## План до рабочего приложения
См. `plans/ROADMAP-app.md`. Ledger: `.superpowers/sdd/app/progress.md` (решения и отложенные мелочи).
Правило: каждый агент работает в своей ветке/worktree, контроллер сливает в `feature/splittun-upstream-proxy`.

Готово (с ревью Opus):
- A1 авторизация UI по процессу (`pm-core/src/api_auth.rs`, `--allowed-clients`, auth вне tokio-воркеров).
- A2 `pm-resolver`: модель, URL, UDP/TCP/DoT/DoH (rustls+ring), выбор резолвера по зонам, кэш `cache:intel/nameRecord/*` (Go читает записи Rust), failover, опции `dns/*`, API `dns/*`. Локальные сети — по адресам интерфейсов (nix getifaddrs), не по маршрутам (VPN 128/1 не ломает зоны).
- A4 `pm-netutils`, `pm-endpoints` (сверка с Go 2000+ случаев), опции профиля (27 шт., байт-в-байт), `GlobalProfileConfig`.
- A5 `pm-network`: Connection (JSON байт-в-байт), ключи `network:tree/*`, хранилище, чистильщик, открытые DNS-запросы. `BandwidthEnabled=true` — как в локальной правке пользователя (отличие от upstream).
- A7 ч.1 `pm-interception`: iptables (дословно), метки, разбор пакетов, nfqueue (единственный `unsafe` — `AsyncFd::register`), conntrack через ctnetlink (rustix). Живой запуск не проверен (нужен root).
- A9 `pm-jess`: проверка подписи индекса intel, проверено на реальном подписанном индексе реальными ключами.
- A10 `packaging/linux/portmaster-core-switch.sh` (проверен только `status`).

- A3 `pm-nameserver`: UDP DNS-сервер, правила пакетов как у miekg, TXT-записи с декодированием экранирования (сверено по байтам с Go), поиск конфликтующего процесса.
- A6a `LayeredProfile` (`pm-profile/src/layered.rs`): локальный профиль поверх глобального конфига, сопоставление правил и фильтр-листов, ревизии.
- A8 `pm-netquery`: язык запросов → SQL (сверено с Go), БД live (в памяти) + `history.db`, upsert, трафик, очистка, JSON строк байт-в-байт как в Go (оба режима: одиночный запрос со схемой и batch без схемы).

В работе:
- A6a исправления после ревью (ранний выход parseConfig как в Go, миграция `MigrateSecurityLevelToBoolean`).
- A8c эндпоинты netquery (`netquery/query`, `query/batch`, графики, очистка истории).
- A7a склейка DNS в pm-core (резолвер + DNS-сервер, фильтр пока пропускает всё).

Проверка на реальных данных (вариант А, копия `/tmp/portmaster-rust-verify`) выполнена: ядро стартует, `/api/v1/ready` отвечает 200, `config/options` и `intel/geoip/countries` отдают данные, `netquery/query` с `databases:["history"]` читает реальную историю. Найден и исправлен баг совместимости: Go пишет nil-слайсы профилей как `null` (`Icons` у 1008 профилей, `Fingerprints` у 7), Rust требовал массив; добавлен `deserialize_null_default` для `Icons`/`Fingerprints`/`Config` и регрессионный тест на реальной форме JSON. Без root ожидаемо: splittun TCPv4 proxy `Permission denied` и предупреждение «filter: not starting interception, root privileges are required». Windows runtime check требует Windows-машины. Linux nfqueue/smoke — проверен пользователем и зафиксирован в TESTING.md. Остатки: sign команда updatemgr (pm-jess умеет только проверять подписи), упрощённый debug/core, часть Windows FFI (GetAdaptersAddresses, SIO_RCVALL, kext) — из-за запрета unsafe и отсутствия Windows.

Находки в Go (стоит знать):
- netquery: алиас `as` проверяется незакреплённой регуляркой, а `databases` подставляется в SQL без проверки → SQL-инъекция через локальный API. В Rust закреплено/ограничено.
- netquery: text-search по неизвестному столбцу → паника (nil); `"select":"строка"` молча игнорируется; `$min` с условием всегда ошибка.
- endpoints: правило `+ # коммент` → паника в Go.
- resolver: записи кэша в Go фактически никогда не истекают в БД (`PutNew` сбрасывает мету).
- netquery: `$field` рядом с `$count`/`$sum` попадает в SQL без проверки (ещё одна инъекция); `$sum.field` рядом с `$distinct` — тоже. В Rust проверяется.
- netquery: `MarkAllHistoryConnectionsEnded`/`CleanupHistory` пишут локальное время, а всё остальное — UTC. В Rust везде UTC.
- netquery: пустой результат отдаётся как `null`, а не `[]` (Rust повторяет).

## Дальше
- T8: проверка на реальных данных (копия `/var/lib/portmaster`) — выполнена, см. выше; баг decoding профилей исправлен.
- M2 закрыт: netstate, process, profile, notifications, runtime, status (+ минимальный netenv).
- M3: загрузчик `service/updates` (worker, download, gzip/zip, upgrade), `customlists`, `nameserver`/resolver.

## Интеграция Xray
- Вместо SPN планируется xray/v2ray. У пользователя есть свой Rust-сервер Xray (отдельный workspace `xray-rs`:
  `xray-core-rs`, `xray-server`, `xray-ffi`, `xray-config`; `forbid(unsafe_code)`, MPL-2.0, MSRV 1.96).
- Вариант A (рекомендуется): запускать его бинарник как дочерний процесс, конфиг генерировать из подписок,
  статистику забирать через его gRPC API. Не тянет зависимости в ядро и не ломает MSRV 1.94.
- Вариант B: линковать через `xray-ffi` — требует `unsafe`/FFI в `pm-core` (запрещено линтом) либо safe-обёртки.
- Вариант C: path-зависимость на крейты — блокируется MSRV (1.96) и лицензией MPL-2.0 (наш проект GPL-3.0).

## Пожелания к проекту (учесть в этапах M3+)
- Вместо SPN — интеграция с xray/v2ray.
- Подписки VPN и карта с точками подключения.
- Максимум без `unsafe`; где возможно — оптимизации Rust, которых нет в Go.
- Совместимость данных и поведения с Go-ядром не ломать.
- Сначала Linux, Windows позже.
- Локализация интерфейса (русский) и более простое меню.

## Напоминания пользователю
- Когда порт будет реально закончен: напомнить пользователю убрать/проверить root-доступ (его просьба). Фактически в этой сессии root у агента не было — работали только с копией `/tmp/portmaster-rust-verify` (вариант А), но напоминание оставить.

## Заметки
- `pm-network`: пустые списки/карты `Entity` пишутся как `null` (Go различает nil и пустое); `TunnelContext` всегда `null`; `DNSContext.Modified/Expires` — строки RFC 3339; `PacketInfo` собственный (сырой номер протокола, адреса могут отсутствовать); профили, IP-инфо резолвера и captive portal инжектируются через `Environment`; воркер очистки и `CleanProcessStorage` вызывает вызывающий код; ожидание домена на Windows не портировано.
- `pm-endpoints`: правило вида `+ # комментарий` в Go паникует (индекс fields[1]), в Rust — ошибка разбора. ASN хранится как u64 (в Go `uint`). Сопоставление country/ASN/списков с глобальными IP в эталонах не генерируется (нужны geoip/filterlists модули) — покрыто Rust-тестами со статической локацией.
- Реальные данные: `/var/lib/portmaster/databases` закрыт для обычного пользователя; проверка проведена на копии `/tmp/portmaster-rust-verify` (вариант А, подготовлен пользователем). В реальной БД nil-слайсы/карты профилей кодируются как `null`; все десериализаторы Rust-профилей это учитывают.
- Парсер запросов Go обрезает UTF-8 побайтно в последнем токене — сравниваем только ok/err.
- `query.json` строится на JSON-wrapper-записях; CBOR-запись фиксирована (`a0`) для детерминизма.
- `pm-database`: `limit/offset/orderby` не применяются (как в Go); кэш интерфейса, `PutMany` и purge не реализованы.
- Относительный TTL, как в Go, хранится в `deleted` (отрицательным) и раскрывается в `expires` при следующем обновлении меты.
- `pm-config`: неизвестные ключи config.json сохраняются (в Go теряются) — осознанное отклонение; функции валидации есть (`ValidationFunc`, A4b), миграции значений не реализованы.
- `pm-config` не регистрирует `core/devMode` и `core/log/level` — их регистрирует ядро при старте.
- `pm-profile`: нет активного кэша профилей, событий/хуков БД, полного `LayeredProfile` (слияние слоёв конфига)
  и разбора правил endpoints; `Profile.New` повторяет го-квирк с нулевым отпечатком в конце после компакции.
- `pm-process`: `GetProcessesWithProfile` и HTTP-эндпоинты процесса не портированы (поиск профиля через
  `Process::find_profile` есть);
  загрузки сериализованы одним мьютексом вместо singleflight; `leader` ищется через кэш, а не ссылкой в процессе.
- `pm-geoip`: база читается в память (`Reader<Vec<u8>>`), а не через mmap — так обходится `unsafe` (никакой функции `open_mmap` в crate НЕТ, предыдущее утверждение было ошибочным);
  базы перезагружаются из intel-индекса воркером ядра (как Go geoip worker по событию
  resources updated); HTTP-эндпоинт `intel/geoip/countries` есть; ASN — `u32` (в Go `uint`).
  Реальные MMDB не входят в репозиторий (лицензия MaxMind), тест `tests/database.rs` включается через
  `PM_GEOIP_V4`/`PM_GEOIP_V6`; локально проверен на `/var/lib/portmaster/intel/*.mmdb` — совпал с Go.
- `pm-updates`: портированы модель индекса, загрузчик, сканер каталога, конфигурация с Go-валидацией,
  применение апгрейда и **модуль апдейтера** (`Updater`: проверка, уведомления «up-to-date/available/ready/restart»,
  auto-download/auto-apply, forced update, восстановление повреждённой установки, событие `resource update`,
  `get_index/get_files/get_file/configure`), каналы обновлений (`configure.rs`). Нет: UI-lock апгрейда (на вызывающем),
  рестарт процесса по callback в ядре не подключён. Отклонение: при пропавшем основном каталоге создаётся он,
  а не purge-каталог (в Go баг); исправлена опечатка «udpating failed».
- `pm-intel`: фильтр-листы: индекс (`index.dsd`, категории/источники/unbreak), планирование и применение
  обновлений (`update_filter_lists`: порядок файлов, base-restart, версия кэша, удаление устаревших записей),
  модульная обвязка (`FilterListsModule`: загрузка bloom из кэша, состояния init/failed/stale, игнор-флаги
  событий). `Entity` использует трейт `LocationProvider` вместо глобального geoip-воркера; порядок списков
  в `MatchLists`/`ListBlockReason` стабильный (BTreeMap) против случайного в Go; `splitDomain` опирается на
  снимок PSL из крейта `psl` (версии могут отличаться от таблицы Go). Декодер принимает только JSON-записи DSDL (в файлах списков используется JSON); записи пишутся
  по одной вместо батчей по 1000; нет индекса списков, загрузчика `updates`, `intel.Entity`/`geoip`.
  `customlists` портированы: опция `filter/customListFile`, разбор файла и поиск (домены/IP/ASN/страны,
  свой `splitDomain`, сверен с Go на 18 доменах), цикл обновления с состояниями/уведомлениями
  (`CustomListService::check_and_update`) и эндпоинт `customlists/update`. Нет воркера по таймеру и подключения
  к `FirewallEnv` (сделает ядро).
- `pm-status`: нет `mgr.Manager`-обвязки (начальные состояния и колбэки добавляет ядро); модули обходятся в
  сортированном порядке, поэтому при равной severity worst state достаётся первому по алфавиту (в Go — случайный).
- `pm-core`: рестарт по callback апдейтера и `core/restart`/`core/shutdown` через `CancellationToken` (graceful stop для супервайзора), UI upgrade lock (`423` при блокировке) и `ui/reload`, метрики вердиктов пакетов по `--write-packet-metrics` (record в `NfqPacket::set_verdict`, запись раз в 5 c в формате Go), эндпоинты `core/paths`, `updates/get/{artifact_path}/{artifact_name}`.
- `pm-core` A7b-3: файрвол собран и запускается в ядре — `CoreProfiles` (ProfileLookup/LayeredProfileLookup/SplitTunnelProfiles), `CoreFirewallEnv` (FirewallEnv/PacketEnv поверх netenv, customlists, резолвера, профилей и splittun), `Firewall` (Network + PacketHandler + netquery Database/Manager + лента соединений + iptables/nfqueue). Запуск перехвата требует `filter/enable` и root; без root ядро работает и тесты проходят. Зарегистрирован netquery API, retention из layered profiles, старт/стоп splittun-прокси по `splittun/enable`, мост уведомлений netenv → pm-notifications. Живой запуск под root не проверялся (нужен пользователь с привилегиями).
- `pm-geoip` (субагент, ветка agent/geoip-location, слит): `location.go` из geoip уже был портирован; недостающая модель `service/netenv/location.go` — `IpVersion`, `DeviceLocation(Source)`, `DeviceLocations` (Best/BestV4/BestV6, сортировка, Go-порядок JSON), timezone-derived location, `DeviceLocationService` с инжектируемым `LocationEnvironment`, pure continent helpers. ICMP traceroute/OS/TZ инжектируются; в ядро пока не подключено.
- Linux live-проверка (пользователь, 2026-11-16): в изолированном netns ядро поднялось, `filter: nfqueue interception started queues=[17040,17140,17060,17160]`, счётчики NFQUEUE ненулевые (6 пакетов/384 байта на очередь), `--write-packet-metrics` содержит строки `...;PermanentAccept;...`; после SIGINT цепочек `PORTMASTER-*` нет. Процедура зафиксирована в `TESTING.md`.
- `pm-core`:board: History — дефолт `history/enable=true` (fork-отклонение от upstream false), портированы аккаунт-эндпоинты `spn/account/user/profile`, `account/features`, иконки; app/profile теперь возвращает реальный профиль вызывающего процесса (AuthToken несёт pid/path). Apprise подключён опционально (`core/appriseURL`), события online-status уходят при наличии URL.
- `pm-updatemgr`: бинарник `updatemgr-rs` — все команды Go портированы: `scan`, `convert`, `download`, `mirror` и `sign` (jess-подпись через `--tsdir`, ключевой формат и текстовый формат; `--tskeyring` даёт явную ошибку). Кросс-проверка подписи через настоящий Go `filesig.VerifyJSONSignature`: `VERIFY OK` для шести конфигураций.
- `pm-debug`: порт `base/utils/debug` — секции `AddSection` байт-в-байт (github style, code fences, line breaks), `debug/core` в `pm-core` теперь использует их и поддерживает `?style=github`.
- `pm-control`: порт `service/control` — pause/resume с авто-resume по таймеру и по включению SPN, уведомления `control:paused`/`resumed`/`error`, эндпоинты `control/pause`/`control/resume` (PERMIT_ADMIN).
- `pm-migration` + `pm-profile::migrations`: порт `base/database/migration` и трёх миграций профиля; статус `core:migrations/profile` = `1.6.3`, как в Go.
- `pm-process::tags`: Linux-хендлеры interpreter/AppImage/snap/flatpak/net, 16 тестов, сверены с Go; подключены к атрибуции процессов в `pm-core`.
- `pm-broadcasts`: порт `service/broadcasts`, YAML-источник, matching через `pm-query`, состояния, три API, periodic task; слит в основную ветку.
- `pm-compat`: Linux-часть `service/compat` — self-check (raw packet/DNS random answer), уведомления о конфликтах, iptables-секция в debug/core, эндпоинт `compat/self-check`. Windows WFP не портирован. Шаг 3 self-check требует подстановки случайного ответа в dns резолвере — пока известный пробел.
- `pm-interop`: `service/interop` + IVPN — wire-протокол, TCP-клиент, ping API, вердикты, уведомления, DNS-координация; 32 теста. `hook_linux.go` **портирован** в `pm-interop/src/hook.rs` (nftables + iptables fallback), но требует root для реальной установки правил.
- `pm-sync`: порт `service/sync` — экспорт/импорт профилей и настроек, JSON/YAML байт-в-байт с Go (включая чексуммы), 6 API, 14 тестов с Go-эталонами.
- Волна 5 (слиты): `pm-api-client` (WS-клиент, reconnect/resuscitation), `pm-utils` (11 хелперов, атомарная запись config.json), `pm-rng` (Fortuna, кросс-векторы с Go), `pm-dnsmonitor` + `pm-firewall::inspection` (Linux varlink + инспекторы вердиктов), `pm-metrics` (реестр + Prometheus + API, байт-в-байт с Go-эталоном).
- Волна 6 (слиты): `pm-log` (base/log: severity, Go-формат, writer, ContextTracer, slog-адаптер, unexpected-log кольцо), push-воркер метрик + process-метрики + IVPN nftables hook, `pm-xray` (подписки → узлы/страны → конфиг → дочерний процесс `xray-rust`, API `/v1/xray/*`), Angular-страница `/xray` с переиспользованной SPN-картой и русский i18n (95 ключей, переключатель в settings).
- Контракт UI↔бэкенд xray приведен к одному виду: массивы для `/subscriptions` и `/nodes`, `nodeId` в `connect`, DELETE `/subscriptions/{id}`, camelCase `subscriptionId`/`country`, доп. поля статуса `connected`/`country`/`since`. Тест `ui_payload_compatibility` это фиксирует.
- Волна 7 (слиты): реальный h2c gRPC StatsService клиент (`GetStats`/`QueryStats`, protobuf, gRPC-status, `/xray/stats`) вместо `NoopTrafficStats`; расширенный парсер подписок (hysteria2, wireguard, SS-плагины, кастомные заголовки/авторизация); русский перевод UI доведен до 95→628 ключей, helptexts.ru.yaml, SPN/dashboard/app-view/monitor/support/dialogs/netquery. При слиянии i18n пользовательские незакоммиченные Angular-правки были временно stash'ены и восстановлены — diff по 4 файлам прежний.
- Проверка после волны 7: `cargo clippy --workspace --all-targets -- -D warnings` чисто, `cargo test --workspace` — 205 наборов ok, 0 FAILED. Angleskiy build:dev в ветке агента — exit 0.
- Волна 8 (слита): live-обновление TS-подписей по `I18nService.language$` (netquery, search overlay, line chart, map-renderer), `LocalizedValueService`/pipe для стран, вердиктов, feature IDs и уведомлений с fallback на исходную строку; +58 ключей. Пользовательские Angular-правки又一次 stash/restore, diff прежний.
- Решение пользователя по Windows/eBPF (2026-11-17): PROVIDED Windows-машину/доступ и разрешил точечный `unsafe` под `cfg(windows)`.
- Волна Windows (слиты): `pm-winkext` (v1 ioctl + v2 command stream, SERVICE lifecycle; Linux-stubs) и `pm-winplatform` (iphlpapi, osdetail, ETW-integration, Windows-теги/иконки; cfg-gated FFI). Корневой `unsafe_code = "forbid"` не тронут: новые крейты не наследуют workspace-таблицу линтов, объявляют `unsafe_code = "deny"` и `#[allow(unsafe_code)]` только на extern-блоках.
- Кросс-проверка `cargo check --target x86_64-pc-windows-gnu -p pm-winkext -p pm-winplatform -p pm-core --all-targets` — exit 0 (2 не-fatal dead_code warnings в pm-core). **Runtime на Windows НЕ проверялся** — жду доступ к ноутбуку.
- Проверка после Windows-слияний: clippy чистый, `cargo test --workspace` — 211 наборов ok, 0 FAILED.
- Открытый вопрос пользователя: после switch на Rust в истории нет приложений, после отката на Go они появляются. Причина найдена ревью и исправлена в `58b9848e`: Rust не создавал локальные профили (`CoreProfiles::layered_for` возвращал None), netquery-фид стартовал только внутри `Firewall::start()` после early return без root/с выключенным фильтром, а DNS-коннекты вызывали `update_features()` до установки `conn.process`. Теперь профили создаются/сохраняются, фид стартует безусловно, процесс Attach'ится раньше.
- Ревью шестью агентами ( hy4-flash-free; попытка DeepSeek Free уперлась в квоту) выявило и исправлено: UTF-8-паника в percent-decode (`pm-xray`, `pm-sync`), строковый `port` в VMess JSON, подписки из UI не обновлялись (`enabled=false` по умолчанию), `raw_config`/URL с секретами в API, обрыв refresh всех подписей на первой сетевой ошибке, лог с телом DB-записи в `pm-api-client`, гонка idx в `pm-interop`, wildcard-роут Angular до `dashboard`. Найдена и убрана галлюцинация STATUS про `open_mmap`; `hook_linux.go` наоборот реально портирован.
- Остатки по честной инвентаризации ревью: eBPF C-программы, Windows kernel driver (C + подпись), `base/template`, альтернативные бэкенды БД (badger/bbolt/fstree/sinkhole), `base/info`, `base/container`, `base/database/{iterator,dbmodule}`, CLI-утилиты `cmds/cmdbase|integrationtest|testsuite|trafficgen`, агрегатор `service/detection`. bbolt-БД не поддерживается вовсе (только sqlite) — для старых инсталляций это отдельный пробел.
- Волна ревью из 20 агентов (hy4-flash-free, read-only): подтверждено и исправлено в `d7f179fc` — `ProfileStore::get` с голым ID, точное имя из binmeta, NULL-time→JSON null, удаление уведомления будит ждущих промпта, `config.json: null`, `Accept` с q/charset, IPv4-mapped geoip. Проверка: clippy чистый, 211 наборов ok. Остальные находки агентов (API key runtime, restart exit 23, DNS hook/fast-track, conntrack cleanup, ICMP raw socket, updater paths, Windows ioctl sizes, splittun bindings, sync/control/broadcast форматы) — в очереди, часть требует отдельной проверки.
- Документация SPEC/STATUS/планов местами устарела (ревью документации нашло 10 расхождений): SPEC всё ещё говорит, что broadcasts/sync/compat/jess не портированы; STATUS содержит старые «нет» про netenv, geoip API, updater worker. Исправлено отдельной порцией правок документации (этот коммит).
- Leptos-пилот: 12 агентов (по 2 на компонент — visual/logic) для shell/lang/counter/xray/nodes/theme; 43 нативных теста зелёные. Переключатель UI добавлен в `portmaster-core-switch.sh`: `ui {status|rust|typescript}`. При `PORTMASTER_UI_TYPE=rust` ядро отдаёт WASM-пилот по `/` и ассеты в корне; иначе — обычный Angular. Smoke: `/`, `/pm_leptos_pilot.js`, `/pm_leptos_pilot_bg.wasm`, `/pilot` — 200 с корректными MIME. Коммит `9894f776`. Angular остаётся production UI; пилот оценочный.
- Финальная проверка после всех слияний: `cargo clippy --workspace --all-targets -- -D warnings` чисто; `cargo test --workspace` — 193 набора ok, 0 FAILED. Ядро стартует на копии реальных данных `/tmp/portmaster-rust-verify`, `/api/v1/ready` → 200; ожидаемые предупреждения: IVPN-демон отсутствует, splittun требует root. Build-артефакты в tmpfs (`target/` → symlink на `/tmp/opencode/pm-main-target`), после проверок cleans; диск `/games/home` — 82 ГиБ свободно.
- `pm-apprise` (субагент, ветка agent/apprise): порт `base/apprise` — модель сообщений, HTTP-отправка с инжектируемым клиентом, teaser из `base/utils/safe.go`, 17 тестов; добавлен в workspace.
- `pm-netenv` (субагент, ветка agent/netenv-remaining): окружение (nameservers/gateways из resolv.conf и /proc, D-Bus/busctl connectivity), ICMP-listener и probe с инжектируемыми сокетами, флаги SPN/DNS, уведомления offline/limited/portal через `NotificationSink`; ~60 тестов.
- `pm-netenv`: состояние online status/captive portal с событиями, **активные проверки**: флаг сетевых изменений,
  кэш интерфейсов (`get_interface` по IP/MAC/имени, физические default-интерфейсы из `/proc/net/*route`,
  `/sys/class/net/*/device`, throttle 1 c), кэш адресов (`assigned/global`, `is_my_ip`, `get_local_network`),
  проверка онлайна (SPN/DNS-шорткаты, assigned-адреса и Android-ignore, HTTP-портал с redirect/200, DNS-проверка,
  точные Go-комментарии), `OnlineMonitor` (старт/триггер/задержки 10/5/60/300 c) и `NetworkChangeMonitor`
  (SHA1-чексумма интерфейсов, 15 c online / 1 c offline, ручные триггеры без online-проверки). Ядро запускает оба
  монитора. Портированы также nameservers/gateways (resolv.conf, /proc), ICMP-listener и probe, уведомления
  offline/limited/portal через `NotificationSink`, D-Bus/busctl connectivity. `SystemInterfaceEnv` связывает кэши с `pm-splittun`.
- `pm-mgr`: `StateMgr` без привязки к `Manager` (имя модуля передаётся явно).
- `pm-runtime`: провайдеры в `BTreeMap` вместо radix-дерева (результаты те же), `Query` возвращает `Vec` вместо
  итератора; второй экземпляр модуля в процессе получает собственный реестр (в Go — ошибка «only one instance»).
- `pm-notifications`: HTTP/WS-эндпоинтов уведомлений нет (в Go их тоже нет — только БД); `EventData` и payload
  действий хранят порядок ключей `serde_json`, Go сортирует ключи (уведомления не персистятся);
  нет привязки к модулям (`module-mirror.go`) и нотификатора рабочего стола.
- `pm-netstate`: нет single-flight для сканов `/proc` (кэш владельцев есть); `Exists` для UDP сравнивает `listens_any` вместо первого байта IP.
- Фикстура `netstate.json` генерируется из живых таблиц, но перед коммитом санируется (документационные IP, фейковые порты/inode/PID/uid).
- A4b: миграции значений (`MigrateSecurityLevelToBoolean`) не портированы; `ResolveListIDs` через `set_list_resolver` (по умолчанию ID как есть); профиль `global-config` не сохраняется; валидация прокси-URL — свой порт `ParseUpstreamProxyURL` (крейт `url`), не `pm-proxy`.

- A3: pm-nameserver (UDP-сервер, поток запроса, ответы nsutil, failing-кэш, конфликт-репорт, dns/listenAddress). Отклонения: метрики не портированы; QueryPortmasterEnv не доступен из ResolverService; reason.context-респондер заменён FilterOutcome.

- Модуль split tunnel (`crates/pm-splittun`): `splittun/enable`, очередь `AwaitRequest`/`consume` с TTL 30 с и очисткой, выбор интерфейса через `InterfaceEnv`, upstream-прокси с Go-валидацией и redacted-строкой, TCP/UDP-прокси на 719 (UDP 127.0.0.17/::1 на Linux), egress-кэш в pm-proxy (`IsProxiedConnectionInfo`), адаптер к `pm_firewall::SplitTunnelEnv`. Нет: метрик прокси, egress для UDP исчезает только по idle-таймауту.
- `pm-firewall`: опции (`filter/*`), `check_split_tunneling`/`request_split_tunneling`, `prompt.go` — `create_prompt`/`prompt` с `PromptEnv` (таймаут, профиль, уведомления, сохранение правила), Go-сообщения/действия, `endpoint_rule`; в `pm-network` добавлены активный промпт соединения и `entity_to_json`.
- `pm-profile`: `Profile::add_endpoint_entry`/`add_endpoint`/`add_service_endpoint` с го-логикой дубликатов и префиксов.
- `pm-core`: зарегистрированы опции файрвола/splittun/customlists и апдейтов (`core/releaseChannel`,
  `core/automaticUpdates`, `core/automaticIntelUpdates`), эндпоинты `intel/geoip/countries`, `customlists/update`,
  `updates/check`, `updates/apply`; работают перезагрузка списка по конфигу и раз в минуту, бинарный и intel-апдейтеры
  (часовая проверка, применение списков к intel-индексу), загрузка bloom из кэша и состояния фильтр-листов.
  Интеграционный тест ядра проверяет всё это по HTTP.
- `pm-intel`: индекс фильтр-листов (`index.dsd`, категории/источники, unbreak, semver-сравнение, кэш и обновление из файла) и планирование обновлений (порядок файлов, base-restart, версия кэша с reset-версией).

- Мелкие порции (вне нумерации A): хэши исполняемых файлов (`pm-process`), `customlists` (`pm-intel`),
  glob-совместимый с `gobwas/glob` (`pm-updates::Glob`, классы с `!`, `**`, отказ на `{a,b}`), сканер индекса
  (`pm-updates::generate_index_from_dir`), конфигурация апдейтера (`UpdaterConfig::check`), применение апгрейда
  (`pm-updates::Upgrader`), эндпоинт стран GeoIP, опции файрвола (`pm-firewall::register_options`) и вердикты
  split-tunnel (`check_split_tunneling`/`request_split_tunneling`, прокси инжектится через `SplitTunnelEnv`).
  Эталоны сканера и стран сняты с Go.

- Волна закрытия находок полного ревью (2026-11-17, без Windows/пользовательских тестов, коммиты
  `b6554c2f`..`80be3137`): conntrack cleanup (`delete_all_marked` на старте/стопе) и
  `reinsert_displaced` по расписанию 5/10/30 c подключены в `Firewall::start/stop`; rollback флага
  `started` при ошибке iptables. `delete_unmarked` **намеренно НЕ вызывается** (коммит `227973d6`):
  в Go это под TODO и нужно только при DNAT-фичах (SPN/split-tunneling), а без них оно рвёт все
  существующие не-loopback-соединения при каждом старте — именно это отключило интернет у
  пользователя при первом switch на Rust. Self-check #3 (коммит `f5b4c60d`): шаг 3 пропускается
  (`dns_response_check=false`), пока в nameserver не подключена подстановка случайного ответа —
  раньше он всегда падал с `failed to receive test response: no such host`, создавая ложное
  уведомление о несовместимости и уводя онлайн-статус в Offline (что блокирует DNS для всех
  доменов, кроме тестовых): объяснение «инета на половину» на Rust-ядре. UDP-прокси: per-session `CancellationToken` +
  `EgressGuard`, relay останавливается при eviction и не обновляет новую сессию того же пира
  (регрессионный тест). `core/apiKeys` перечитываются из конфига при каждом изменении (как Go
  `EventConfigChange`), регрессионный тест. Xray: `plugin`/`pluginOpts` Shadowsocks больше не
  эмитируются (xray-rs их отвергает), warning + тест. Интерактивный prompt: создаётся настоящее
  уведомление через `create_prompt` + `PromptEnv` ядра, правило сохраняется action-хендлером;
  текущий пакет deny'ится с причиной Go-timeout (асинхронное применение вердикта остаётся только в
  тестах — синхронный хук не может ждать). Спец-профили (7 ID, конфиг/name/internal как в Go) и
  tag-based профили (AppImage/Flatpak/interpreter/snap/net) создаются в `CoreProfiles::layered_for`
  до совпадения по отпечаткам, 3 теста. clippy чистый, `cargo test --workspace` — 212 наборов ok,
  0 FAILED. **Осталось (требует винды/тестов пользователя/внешнего кода):** runtime-проверка
  Windows, eBPF C/Windows driver, bbolt, реальный Xray StatsService (нужен `xray-server`), полная
  async-подстановка вердикта промпта в текущее соединение, проверка switch/истории и `ui {status,rust,typescript}`.
