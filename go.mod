module worktimesync

go 1.26

// Зависимости, реально импортируемые в коде на спринте 1 день 1.
// Остальные (golang-ical, go-webdav, rrule-go, oauth2, jwt) добавим
// в спринте 1 день 2-3 по факту использования.
require (
	github.com/caarlos0/env/v11 v11.4.1
	github.com/gofiber/fiber/v3 v3.0.0
	github.com/google/uuid v1.6.0
	github.com/hibiken/asynq v0.25.1
	github.com/jackc/pgx/v5 v5.7.6
	github.com/redis/go-redis/v9 v9.16.0
	github.com/rs/zerolog v1.34.0
)

require (
	github.com/arran4/golang-ical v0.3.5
	github.com/emersion/go-webdav v0.7.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/teambition/rrule-go v1.8.2
	golang.org/x/crypto v0.48.0
)

require (
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/emersion/go-ical v0.0.0-20240127095438-fc1c9d8fb2b6 // indirect
	github.com/gofiber/schema v1.6.0 // indirect
	github.com/gofiber/utils/v2 v2.0.0 // indirect
	github.com/golang-migrate/migrate/v4 v4.19.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/richardlehane/mscfb v1.0.6 // indirect
	github.com/richardlehane/msoleps v1.0.6 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/spf13/cast v1.7.0 // indirect
	github.com/tiendc/go-deepcopy v1.7.2 // indirect
	github.com/tinylib/msgp v1.6.3 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.69.0 // indirect
	github.com/xuri/efp v0.0.1 // indirect
	github.com/xuri/excelize/v2 v2.10.1 // indirect
	github.com/xuri/nfp v0.0.2-0.20250530014748-2ddeb826f9a9 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	google.golang.org/protobuf v1.36.7 // indirect
	gopkg.in/telebot.v3 v3.3.8 // indirect
)
