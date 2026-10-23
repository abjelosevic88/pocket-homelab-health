# Configuration

Environment variables; every one has a default. Personal settings (units, goals, connections, notifications) live in the UI per person — the `*_URL`/`*_TOKEN`/`OPENAI_*`/`NOTIFY_*` values below are copied once into the first admin's settings.

| Variable | Default | What |
|---|---|---|
| `PORT`, `HOST` | `8080`, `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL (links in pushes, the OIDC callback) |
| `DATA_DIR` | `./data` | Database, uploads, Core token |
| `DB_PATH` | `$DATA_DIR/pocket-health.db` | SQLite file |
| `FILES_DIR` | `$DATA_DIR/files` | Temporary import uploads |
| `MAX_UPLOAD_BYTES` | `4294967296` | Largest Apple Health export accepted (4 GB) |
| `MAX_BODY_BYTES` | `67108864` | Largest JSON push (64 MB) |
| `UNITS` | `metric` | Unit system new people start with (`metric`/`imperial`) |
| `TZ` | – | Time zone: the day a sample belongs to follows the offset it was sent with; times without one use this |
| `AUTH_MODE` | `local` | `local` (accounts), `proxy` (trust `AUTH_PROXY_HEADER`/`AUTH_PROXY_EMAIL_HEADER`), `none` |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | – | Seed the first admin while the database is empty |
| `SESSION_TTL`, `SECURE_COOKIES` | `720h`, `false` | Set `SECURE_COOKIES=true` behind HTTPS |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | – | Sign in with an OpenID Connect provider (not needed with Pocket Core) |
| `NOTIFY_URL`, `NOTIFY_FORMAT` | – | Apprise (`…/notify/key`), ntfy topic URL or JSON webhook; format `apprise`/`ntfy`/`json` |
| `HABITS_URL`, `HABITS_TOKEN` | – | Pocket Habits, direct connection (or use Pocket Core) |
| `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` | – | The recap and questions without Pocket AI |
| `CORE_URL`, `CORE_REGISTRATION_KEY`, `CORE_APP_URL` | – | Pocket Core |
| `TRUST_PROXY`, `METRICS_ENABLED`, `LOG_LEVEL`, `LOG_FORMAT` | `true`, `true`, `info`, `text` | |
| `DEMO_DATA` | `false` | Seed 90 days of plausible data into an empty owner account |

Commands: `pocket-health backup [-o FILE]`, `pocket-health reset-password [-email] [-password]`, `pocket-health version`.
