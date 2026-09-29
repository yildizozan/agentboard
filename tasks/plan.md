# Uygulama Planı: agentboard

## Genel bakış

`agentboard`, kodlama agent'larının çalışırken repo bazlı iş listesi tutmasını sağlayan bir MCP sunucusu, CLI ve web board'dur. Görevler dört durumdan geçer: `backlog`, `todo`, `doing`, `done`. Agent'lar MCP tool'ları ile görev ekler, listeler, taşır, düzenler ve siler. İnsan aynı veriyi CLI ile veya tarayıcıdaki kanban board ile görür ve yönetir; board tüm repolar arasında seçici sunar. Veri tek bir SQLite dosyasında (`~/.agentboard/agentboard.db`) tutulur ve oturumlar arasında kalıcıdır.

Tek binary, CGO yok. Aynı cobra uygulaması `serve` (MCP stdio), `board` (yerel web arayüzü) ve insan komutlarını sunar. Board'un Vite build çıktısı binary'nin içine `embed` edilir. Dağıtım goreleaser ile GitHub Releases üzerinden yapılır.

## Mimari kararlar

### Depolama

- **SQLite, tek dosya:** `~/.agentboard/agentboard.db`. `AGENTBOARD_HOME` env değişkeni dizini değiştirir (testler ve özel kurulumlar için).
- **Neden SQLite:** Her agent oturumu kendi MCP process'ini başlatır; aynı DB'ye birden çok process aynı anda yazar. SQLite WAL modu ve `busy_timeout` bunu güvenli şekilde çözer. Markdown, JSON ve görev başına dosya seçenekleri değerlendirildi ve elendi (gerekçe sohbet geçmişinde; özet: lock ve parser yükü, ya da ID üretimi ve frontmatter maliyeti).
- **Driver:** `modernc.org/sqlite` (saf Go, CGO yok, cross-compile kolay). `mattn/go-sqlite3` CGO gerektirdiği için seçilmedi.
- **DSN pragmaları:** `_pragma=busy_timeout(5000)&_txlock=immediate`. `immediate` yazma transaction'larının başta kilit almasını sağlar; okuma-kontrol-yazma adımlarında deadlock ve `SQLITE_BUSY` yükseltme hatası olmaz.
- **WAL DSN'de değil:** `journal_mode` değişikliği `busy_timeout`'u dikkate almıyor; başka bir bağlantı kilit tutarken hemen `SQLITE_BUSY` dönüyor (Görev 2'de ölçüldü). WAL dosyada kalıcı olduğu için `Open` içinde bir kez, `SQLITE_BUSY` hatasında 5 saniyeye kadar yeniden denenerek açılır.
- **Process başına tek bağlantı:** `db.SetMaxOpenConns(1)`. Process içindeki yazmalar `database/sql` havuzunda sıraya girer; SQLite kilidi için sadece process'ler yarışır. Aksi halde 100 eşzamanlı yazmada ara sıra `SQLITE_BUSY` görüldü.
- **Şema:** `embed` edilmiş tek `schema.sql`, her `Open`'da `CREATE ... IF NOT EXISTS` ile uygulanır. Proje geliştirme aşamasında olduğu için migration yok; şema değişince yerel DB silinir.

### Veri modeli

```sql
CREATE TABLE tasks (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  repo        TEXT    NOT NULL,
  body        TEXT    NOT NULL,
  status      TEXT    NOT NULL,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_tasks_repo_status ON tasks(repo, status);
```

- **`body`:** Kartın tek içeriği Markdown'dır. İlk satır `# <başlık>` olmak zorundadır; düz ilk satır başlığa çevrilir, `##` ile başlayan ya da boş başlık reddedilir (`task.ValidateBody`). Başlık ayrı saklanmaz, `task.TitleOf` ile body'den türetilir (DRY).
- **`AUTOINCREMENT`:** Silme agent'lara açık olduğu için şart. Olmazsa en büyük ID silindiğinde aynı numara yeni göreve verilir ve eski `#15`'e referans veren bir agent yanlış görevi taşıyabilir.
- **`status` için `CHECK` yok:** Geçerli durum listesinin tek kaynağı Go tarafındaki `task.ParseStatus` (DRY). Tüm yazmalar bu fonksiyondan geçer.
- **Zamanlar:** Unix saniye (`INTEGER`).
- **ID global:** Repo başına numara tutulmaz (KISS). Ancak her sorgu `WHERE repo = ?` içerir; bir repodaki agent, global ID ile başka bir reponun görevine dokunamaz.
- **Repo listesi:** Ayrı bir `repos` tablosu yok. Board'un seçicisi `SELECT repo, COUNT(*) FROM tasks GROUP BY repo` ile beslenir. Tüm görevleri silinen repo seçiciden kaybolur; bu kabul edilir.
- **Bilerek dışarıda bırakılanlar (YAGNI):** priority, kart sıralaması, tag, alt görev, yorum, görev sahibi, WIP limiti, arşiv.

### Repo kimliği

- Anahtar `git rev-parse --path-format=absolute --git-common-dir` çıktısından türetilir. Normal repoda common dir `.git` olduğu için anahtar onun parent dizinidir; worktree'ler ana repo ile aynı anahtarı üretir, yani aynı listeyi paylaşır. Submodule ve bare repoda common dir `.git` adında değildir (ör. `super/.git/modules/a`); anahtar common dir'in kendisidir. Her zaman parent alınsaydı aynı superproject'teki iki submodule `super/.git/modules` anahtarında birleşirdi (Görev 3'te testle yakalandı).
- `git` bulunamazsa `Resolve` hata döner. Sessizce dizin yoluna düşmek, aynı repoyu alt dizinlere göre birden çok board'a bölerdi.
- Dizin bir git reposu değilse anahtar, dizinin mutlak yoludur.
- Her iki durumda da `filepath.EvalSymlinks` uygulanır (macOS'ta `/var` ve `/private/var` aynı anahtarı üretmeli).
- **Hangi dizinden çözüleceği:**
  - **MCP:** Agent her tool çağrısında kendi çalışma dizinini zorunlu `cwd` parametresiyle gönderir; server anahtarı her çağrıda bu dizinden çözer. Server process'inin kendi `cwd`'si kullanılmaz. Böylece client server'ı hangi dizinde başlatırsa başlatsın doğru repo bulunur, oturum içinde başka bir repoda çalışan agent doğru listeye yazar ve tek bir server birden çok repoya hizmet eder.
  - **CLI:** `--repo <dir>` flag'i; verilmezse process'in `cwd`'si.
- `cwd` mutlak yol olmalı ve var olan bir dizini göstermeli; aksi halde tool hatası döner. Alt dizin veya worktree yolu gönderilmesi sorun değildir, `Resolve` hepsini aynı repo anahtarına indirger.
- Git için `exec git` kullanılır; go-git bağımlılığı eklenmez.
- Bilinen bedel: repo taşınır veya yeniden clone edilirse eski liste yeni path ile eşleşmez.

### Durumların anlamı

Bu tanımlar tool açıklamalarında ve README'de aynen yer alır; agent'ların durumları tutarlı kullanması buna bağlıdır.

- `backlog`: fikir veya ileride yapılacak, henüz planlanmamış iş.
- `todo`: planlanmış, sıradaki iş.
- `doing`: şu anda aktif olarak çalışılan iş.
- `done`: tamamlanmış iş.

Durumlar arası geçiş serbesttir (`done` durumundan geri açmak dahil). Sadece durum değerinin geçerliliği doğrulanır.

### MCP arayüzü

SDK: `github.com/modelcontextprotocol/go-sdk` (resmi SDK, typed `mcp.AddTool`). Taşıma: stdio.

Her tool ayrıca zorunlu `cwd` parametresi alır (agent'ın çalışma dizininin mutlak yolu). Tabloda tekrarlanmamıştır.

| Tool | Parametreler | Davranış |
|---|---|---|
| `task_add` | `body` (zorunlu, Markdown), `status?` (varsayılan `backlog`) | Görev ekler, oluşan satırı döner |
| `task_list` | `status?` | Verilmezse `done` hariç tüm görevler, her biri tek başlık satırı. Sıra: durum sırası, sonra `id` |
| `task_get` | `id` | Durum satırı ve tam Markdown body |
| `task_move` | `id`, `status`, `from?` | `from` verilirse compare-and-swap: görev şu an `from` durumunda değilse değişiklik yapılmaz ve mevcut durum hata mesajında döner |
| `task_update` | `id`, `body` | Body'nin tamamını değiştirir |
| `task_delete` | `id` | Görevi kalıcı olarak siler |

- **Taşıma ve düzenleme tek store metodundan geçer.** `task.Patch` (`Body`, `Status`, `From`; hepsi opsiyonel) ve `store.Update(ctx, repo, id, patch)` tek bir immediate transaction içinde çalışır. `task_move`, `task_update`, CLI `mv`/`edit` ve HTTP `PATCH` bu metodu çağırır (DRY). MCP'de iki ayrı tool olmalarının nedeni agent için anlamın net kalmasıdır (ISP).

- Çıktı kompakt metindir: `#12 [doing] Fix login bug`. Satır formatı `task.Task.String()` içinde tek yerde tanımlanır; CLI ve MCP aynısını kullanır (DRY).
- Domain hataları (bulunamadı, durum çakışması, geçersiz durum) protokol hatası olarak değil, tool hatası (`IsError`) olarak döner; agent mesajı okuyup karar verebilir. SDK'nın handler hatasını nasıl eşlediği uygulama sırasında dokümantasyondan doğrulanacak.
- stdout MCP protokolüne aittir. Tüm log ve tanı çıktısı stderr'e gider.

### CLI arayüzü

```
agentboard add <body|-> [-s status]
agentboard ls [-s status]
agentboard show <id>
agentboard mv <id> <status> [--from status]
agentboard edit <id> <body|->
agentboard rm <id>
agentboard serve
agentboard board [--addr 127.0.0.1:7420]
agentboard --version
```

Global flag: `--repo <dir>`. Env: `AGENTBOARD_HOME`.

### Board (web arayüzü)

**Komut:** `agentboard board` yerel bir HTTP server açar ve URL'yi yazar: `http://127.0.0.1:7420/?repo=<çözülen repo>`. Varsayılan adres `127.0.0.1:7420`; `--addr` ile değişir. Loopback olmayan bir adres verilirse komut hata ile çıkar.

**HTTP API** (`internal/httpapi`, standart `net/http.ServeMux` pattern'leri; router kütüphanesi yok):

| İstek | Gövde veya parametre | Yanıt |
|---|---|---|
| `GET /api/repos` | | `[{path, name, count}]`; `name` path'in son parçası |
| `GET /api/tasks?repo=` | | `{statuses, tasks}`; tüm durumlar dahil |
| `POST /api/tasks?repo=` | `{body, status?}` | `201` ve oluşan görev; yanıtta türetilmiş `title` da bulunur |
| `PATCH /api/tasks/{id}?repo=` | `{status?, from?, body?}` (gövde doğrudan `task.Patch`'e eşlenir) | Güncel görev; `from` uyuşmazsa `409` ve `current` alanında mevcut durum |
| `DELETE /api/tasks/{id}?repo=` | | `204` |

- Repo tüm görev isteklerinde `?repo=` query parametresiyle verilir; gövdeler sadece görev alanlarını taşır ve bilinmeyen alanlar `400` döner.
- Handler'lar CLI ve MCP ile aynı `store` metotlarını ve `task` kurallarını çağırır (DRY). Domain hataları HTTP koduna tek bir fonksiyonda eşlenir: bulunamadı `404`, çakışma `409`, doğrulama `400`. Doğrulama hataları `task.ErrInvalid` ile eşleşir (`errors.Is`); böylece tüm katmanlar aynı ayrımı yapar.
- **Durum listesi tek kaynaktan gelir:** `GET /api/tasks` yanıtındaki `statuses` alanı sütunların sırasını ve adlarını belirler. Frontend durum listesini kendi içinde tekrar tanımlamaz.

**Güvenlik.** Yerel HTTP server, tarayıcıda açık başka bir sitenin isteklerine açıktır. Önlemler:
- Server sadece loopback adresine bind edilir.
- `Host` header'ı `127.0.0.1:<port>` veya `localhost:<port>` değilse istek `403` ile reddedilir (DNS rebinding önlemi).
- `GET` dışındaki isteklerde `Content-Type: application/json` zorunludur. Başka bir sitenin JSON isteği CORS preflight gerektirir; server CORS header'ı döndürmediği için tarayıcı isteği engeller (CSRF önlemi).

**Frontend:**
- Vite ve vanilla TypeScript (`npm create vite@latest web -- --template vanilla-ts`). Framework yok (YAGNI); state yönetimi büyürse Preact veya Svelte değerlendirilir.
- Üstte repo seçici. Seçim URL'deki `?repo=` parametresinde tutulur (`history.replaceState`); parametre yoksa ilk repo seçilir.
- 4 sütun ve sütun başlığında görev sayısı. Kartta `#id`, başlık ve açıklamanın ilk satırı.
- Native HTML5 drag-and-drop ile taşıma. İstekte `from` = kartın alındığı sütun. `409` gelirse kullanıcıya kısa bir mesaj gösterilir ve liste yenilenir; agent'ın yaptığı değişikliğin üstüne sessizce yazılmaz.
- Görev ekleme formu (başlık, açıklama; varsayılan durum `backlog`), karta tıklayınca başlık ve açıklama düzenleme, onaylı silme.
- **Canlı güncelleme:** 2 saniyede bir `GET /api/tasks`. Sürükleme sırasında ve sekme gizliyken durur; düzenlenmekte olan kartın içeriğini ezmez. Yetmezse ileride SSE ve `PRAGMA data_version` ile değiştirilir.
- Frontend unit testi yok (YAGNI). Doğrulama `tsc` tip kontrolü, `vite build` ve tarayıcıda manuel kontrol ile yapılır. UI büyürse Vitest eklenir.

**Embed ve build:**
- Vite çıktısı `web/dist`'e yazılır. `//go:embed` üst dizine çıkamadığı için gömme kodu `web/embed.go` (paket `web`) içindedir: `//go:embed all:dist`.
- `web/dist/.gitkeep` commit edilir, geri kalan içerik `.gitignore`'dadır. Böylece Node kurulu olmadan da `go build` ve `go test ./...` çalışır. Vite build `dist`'i boşalttığı için npm `build` script'i sonunda `.gitkeep`'i yeniden oluşturur.
- `dist/index.html` yoksa board, UI'nin build edilmediğini ve build komutunu söyleyen düz bir sayfa gösterir. API yine çalışır.
- Geliştirmede `vite.config.ts` içindeki `server.proxy`, `/api` isteklerini `127.0.0.1:7420`'a yönlendirir.

### Dağıtım

- **goreleaser** (`.goreleaser.yaml`, `version: 2`). `before.hooks`: `npm --prefix web ci` ve `npm --prefix web run build`. Build: `./cmd/agentboard`, `CGO_ENABLED=0`; hedefler `linux/amd64`, `windows/amd64`, `darwin/arm64`. Arşiv yok; ham binary'ler `agentboard_<os>_<arch>` adıyla yüklenir (Windows'ta `.exe`). Sürüm `-ldflags` ile cobra'nın `Version` alanına verilir (`agentboard --version`).
- **GitHub Actions:**
  - `ci.yml`: her push ve PR'da `go test -race ./...` ve `npm --prefix web ci && npm --prefix web run build`.
  - `release.yml`: `v*` tag'inde goreleaser ile GitHub Release oluşturur.
- **Repo:** public `github.com/yildizozan/agentboard`. Repo oluşturma ve ilk push kullanıcının açık onayıyla yapılır (Görev 14). Public repo için `LICENSE` dosyası şarttır; lisans yoksa kod varsayılan olarak "tüm hakları saklı" kalır.
- **Windows:** İlk release'e alındı (`windows/amd64`). CI Windows için sadece çapraz derleme ve `go vet` çalıştırır; testler Linux'ta koşar, Windows'ta çalışma zamanı testi yoktur. Windows'ta geliştirme yapılacaksa npm `build` script'indeki `touch` platformdan bağımsız bir komutla değiştirilir.
- **Sonraki sürüm:** Homebrew tap. Homebrew tap ayrı bir `yildizozan/homebrew-tap` reposu ve goreleaser'da `brews` bölümü ister.

### Paket yapısı

```
cmd/agentboard/main.go      cli.Execute() çağırır
internal/task/              Task, Status, ParseStatus, ActiveStatuses, Patch, başlık doğrulama, String(), domain hataları
internal/store/             SQLite: Open, Add, Get, List, Update, Delete, Repos (schema SQL embed)
internal/repo/              Resolve(dir): repo anahtarı
internal/mcpserver/         New(store) *mcp.Server; tool kaydı ve ince handler'lar; repo her çağrıda `cwd`'den çözülür
internal/httpapi/           New(store, ui fs.FS) http.Handler; API, statik dosyalar, güvenlik middleware'i
internal/cli/               cobra komutları; store ve repo'yu kurup komutlara verir
web/                        Vite projesi (src/, index.html, vite.config.ts) ve embed.go
```

- **SRP:** Domain kuralları (`task`), kalıcılık (`store`), repo tespiti (`repo`) ve taşıma katmanları (`mcpserver`, `httpapi`, `cli`) ayrı değişim nedenlerine sahiptir.
- **DRY:** CLI, MCP ve HTTP API aynı `store` metotlarını ve aynı `task` kurallarını çağırır. Durum listesi, doğrulama ve metin çıktı formatı tek yerdedir. Frontend durum listesini API'den alır.
- **DIP ve YAGNI dengesi:** Başlangıçta `Store` interface'i yoktur; tüketiciler somut `*store.Store` kullanır. Testler gerçek SQLite ve `t.TempDir()` ile çalışır. `httpapi` UI dosyalarını `fs.FS` olarak alır; testlerde `fstest.MapFS` verilir. İkinci bir store implementasyonu gerçekten gerekirse interface tüketici tarafında çıkarılır.
- **ISP:** Çok amaçlı tek bir tool veya endpoint yerine küçük, odaklı tool'lar ve endpoint'ler.

### Bağımlılıklar

| Bileşen | Sürüm (2026-09-29 itibarıyla en son) |
|---|---|
| `github.com/spf13/cobra` | v1.10.2 |
| `github.com/modelcontextprotocol/go-sdk` | v1.8.0 |
| `modernc.org/sqlite` | v1.60.0 |
| `vite` (npm) | 8.3.1 |
| goreleaser | v2.18.2 (makinede kurulu değil; CI'da `goreleaser/goreleaser-action` ile çalışır, yerel snapshot için `brew install goreleaser`) |

Go 1.27 (makinede 1.27.1). Node 24 (makinede v24.21.0). Module path: `github.com/yildizozan/agentboard`.

## Görev listesi

Ayrıntılar, kabul kriterleri ve doğrulama adımları `tasks/todo.md` içindedir. Sıra:

- Faz 1, temel: Görev 1 (iskelet ve `task` paketi), Görev 2 (store: Add ve List), Görev 3 (repo tespiti)
- Kontrol noktası: temel
- Faz 2, CLI: Görev 4 (`add` ve `ls`), Görev 5 (store: Update ve Delete), Görev 6 (`mv`, `edit`, `rm`)
- Kontrol noktası: CLI uçtan uca
- Faz 3, MCP: Görev 7 (`serve`, `task_add`, `task_list`), Görev 8 (`task_move`, `task_update`, `task_delete`)
- Kontrol noktası: gerçek agent entegrasyonu
- Faz 4, board: Görev 9 (store `Repos` ve HTTP API), Görev 10 (`board` komutu ve embed), Görev 11 (Vite iskeleti ve salt okunur board), Görev 12 (sürükle-bırak ve çakışma), Görev 13 (ekleme, düzenleme, silme)
- Kontrol noktası: board uçtan uca
- Faz 5, dağıtım ve dokümantasyon: Görev 14 (CI, goreleaser, `--version`, public repo), Görev 15 (README, agent talimatı, `AGENTS.md`)
- Kontrol noktası: tamamlandı

Faz 3 ve Faz 4 yalnızca Faz 2'ye bağlıdır; paralel yürütülebilir. Yüksek riskli kısımlar (eşzamanlı yazma, worktree tespiti) Faz 1'de ele alınır ve erken başarısız olur.

## Riskler ve önlemler

| Risk | Etki | Önlem |
|---|---|---|
| Agent'lar tool'u kullanmaz veya built-in TodoWrite ile karıştırır | Yüksek | Tool açıklamalarında ne zaman kullanılacağı yazılır. README'de `AGENTS.md`/`CLAUDE.md` için hazır talimat snippet'i verilir |
| stdout'a yazılan tek bir satır MCP protokolünü bozar | Yüksek | Tüm log stderr'e gider. MCP testleri in-memory transport ile protokol üzerinden çalışır |
| Tarayıcıdaki başka bir site yerel board API'sine istek atar (CSRF, DNS rebinding) | Yüksek | Loopback bind, `Host` kontrolü, yazma isteklerinde JSON zorunluluğu, CORS header'ı yok. Görev 9'da test edilir |
| Eşzamanlı yazmada `SQLITE_BUSY` | Orta | WAL, `busy_timeout(5000)`, `_txlock=immediate`. Görev 2'de iki ayrı `Store` örneğiyle eşzamanlılık testi |
| İki agent, ya da bir agent ve board, aynı görevi aynı anda taşır | Orta | `task_move` ve `PATCH` içinde opsiyonel `from` ile compare-and-swap |
| Global ID ile başka reponun görevine erişim | Orta | Tüm okuma ve yazma sorguları `repo` ile filtrelenir. Görev 5, 8 ve 9'da test edilir |
| `web/dist` eksikken `go build` veya `go test` kırılır | Orta | Commit edilmiş `web/dist/.gitkeep` ve `all:dist` embed pattern'i. UI build edilmemişse bilgilendirici sayfa |
| Agent yanlış, göreli veya var olmayan bir `cwd` gönderir | Orta | Tool açıklaması mutlak çalışma dizinini ister. Göreli veya var olmayan yol tool hatası döner. Alt dizin ve worktree yolları `Resolve` ile aynı repoya indirgenir |
| Release binary'si UI'siz yayınlanır | Orta | goreleaser `before.hooks` UI'yi build eder. Görev 14'te snapshot binary'nin board'u sunduğu kontrol edilir |
| Repo taşınınca liste kopar | Düşük | Kabul edilir, README'de belgelenir |
| `done` görevleri birikir | Düşük | `task_list` varsayılan olarak `done` göstermez. Board'da `done` sütunu uzarsa ileride limit eklenir |

## Açık sorular

- Yok. Lisans MIT olarak seçildi ve ilk commit ile eklendi.
