# agentboard görev listesi

Tasarım kararları ve gerekçeler: `tasks/plan.md`.

Her görev için ortak bitiş koşulu: `gofmt -l .` boş çıktı verir, `go vet ./...` temiz, `go test ./...` geçer, `go build ./cmd/agentboard` başarılı. Frontend değişikliği içeren görevlerde ek olarak `npm --prefix web run build` hatasız biter.

## Faz 1: Temel

### Görev 1: Proje iskeleti ve `task` paketi

**Açıklama:** Go modülünü, git reposunu ve cobra root komutunu kurar. Domain kurallarını tek kaynakta toplayan `task` paketini yazar: `Status` tipi, `ParseStatus`, `ActiveStatuses` (`backlog`, `todo`, `doing`), başlık doğrulama, `Task.String()` ve domain hataları (`ErrNotFound`, durum çakışması hatası).

**Kabul kriterleri:**
- [x] `agentboard --help` root komutunu ve global `--repo` flag'ini gösterir.
- [x] `ParseStatus` dört geçerli değeri kabul eder; diğer değerlerde geçerli değerleri listeleyen bir hata döner.
- [x] Boş veya sadece boşluk içeren başlık reddedilir; `Task.String()` çıktısı `#12 [doing] Fix login bug` formatındadır.

**Doğrulama:**
- [x] `go test ./internal/task/...`
- [x] `go build ./cmd/agentboard && ./agentboard --help`

**Bağımlılıklar:** Yok. Module path: `github.com/yildizozan/agentboard`.

**Dokunulacak dosyalar:**
- `go.mod`, `.gitignore`
- `cmd/agentboard/main.go`
- `internal/cli/root.go`
- `internal/task/task.go`, `internal/task/task_test.go`

**Kapsam:** S

### Görev 2: Store: Open, migration, Add, List

**Açıklama:** SQLite store'u yazar. `Open(path)` dizini `0700` ile oluşturur, DSN pragmalarını uygular (`journal_mode(WAL)`, `busy_timeout(5000)`, `_txlock=immediate`) ve `PRAGMA user_version` ile embed edilmiş şemayı uygular. `Add` ve `List` her zaman `repo` ile çalışır. `List` durum filtresi alır ve sonuçları durum sırasına, sonra `id`'ye göre döner.

**Kabul kriterleri:**
- [x] Aynı dosyayı iki kez açmak migration'ı tekrar uygulamaz; `user_version` 1'dir.
- [x] Bir repoya eklenen görev başka bir reponun `List` sonucunda görünmez.
- [x] Aynı DB dosyası üzerinde iki ayrı `Store` örneği, her biri 50 goroutine ile eşzamanlı `Add` yaptığında 100 kaydın tamamı hatasız yazılır.

**Doğrulama:**
- [x] `go test -race ./internal/store/...`

**Bağımlılıklar:** Görev 1

**Dokunulacak dosyalar:**
- `internal/store/store.go`
- `internal/store/schema_v1.sql`
- `internal/store/store_test.go`

**Kapsam:** M

### Görev 3: Repo tespiti

**Açıklama:** `repo.Resolve(dir)` fonksiyonunu yazar. Git reposunda anahtar, `git rev-parse --path-format=absolute --git-common-dir` çıktısının parent dizinidir. Git reposu değilse dizinin mutlak yoludur. İki dalda da `filepath.EvalSymlinks` uygulanır (macOS'ta `/var` ile `/private/var` aynı anahtarı üretmeli).

**Kabul kriterleri:**
- [x] Repo kökü ve bir alt dizini aynı anahtarı döner.
- [x] `git worktree add` ile oluşturulan worktree ana repo ile aynı anahtarı döner.
- [x] Git olmayan dizin, symlink'leri çözülmüş mutlak yolunu döner.
- [x] Aynı superproject'teki iki submodule farklı anahtar döner; göreli, var olmayan veya dizin olmayan yol hata verir.

**Doğrulama:**
- [x] `go test ./internal/repo/...` (testler `t.TempDir()` içinde `git init` ve `git commit --allow-empty` çalıştırır; git kullanıcı bilgisi env ile verilir)

**Bağımlılıklar:** Görev 1

**Dokunulacak dosyalar:**
- `internal/repo/repo.go`
- `internal/repo/repo_test.go`

**Kapsam:** S

### Kontrol noktası: Temel
- [x] Tüm testler `-race` ile geçer.
- [x] Eşzamanlılık testi en az 10 kez tekrarlandığında kararlıdır (`go test -race -count=10 ./internal/store/...`).
- [ ] İnsan incelemesi, sonra Faz 2.

## Faz 2: CLI

### Görev 4: CLI `add` ve `ls`

**Açıklama:** İlk uçtan uca dilim. Root komutun `PersistentPreRunE` adımı DB dizinini çözer (`AGENTBOARD_HOME`, yoksa `~/.agentboard`), store'u açar ve repoyu `--repo` flag'inden veya `cwd`'den çözer. `add <title> [-d] [-s]` görevi ekleyip satırını yazar. `ls [-s]` filtre verilmezse `task.ActiveStatuses` kullanır. Komutlar çıktıyı `cmd.OutOrStdout()` üzerinden yazar.

**Kabul kriterleri:**
- [x] `agentboard add "X"` görevi `backlog` durumunda ekler ve `#1 [backlog] X` yazar.
- [x] `agentboard ls` `done` görevlerini göstermez; `agentboard ls -s done` sadece `done` görevlerini gösterir.
- [x] Geçersiz `-s` değeri anlaşılır bir hata ve sıfırdan farklı exit code verir.

**Doğrulama:**
- [x] `go test ./internal/cli/...` (root komut, geçici `AGENTBOARD_HOME` ve geçici git reposu ile çalıştırılır)

**Bağımlılıklar:** Görev 2, Görev 3

**Dokunulacak dosyalar:**
- `internal/cli/root.go`
- `internal/cli/add.go`, `internal/cli/ls.go`
- `internal/cli/cli_test.go`

**Kapsam:** M

### Görev 5: Store: Update (patch ve compare-and-swap) ve Delete

**Açıklama:** `task` paketine `Patch` tipini ekler: `Title`, `Description`, `Status` ve `From` alanları opsiyoneldir. `Patch.Validate()` boş patch'i, `Status` olmadan verilen `From`'u ve geçersiz başlığı reddeder. `store.Update(ctx, repo, id, patch)` tek bir immediate transaction içinde görevi okur. Görev bulunamazsa `ErrNotFound` döner. `From` verildiyse ve mevcut durum farklıysa mevcut durumu içeren çakışma hatası döner. Aksi halde sadece verilen alanları ve `updated_at`'i günceller. Taşıma ve düzenleme aynı metodu kullanır (DRY). `Delete(ctx, repo, id)` görevi siler veya `ErrNotFound` döner.

**Kabul kriterleri:**
- [x] `From` yanlışsa görev değişmez ve hata mesajı mevcut durumu içerir.
- [x] Sadece başlık içeren bir patch durumu ve açıklamayı değiştirmez; boş patch ve `Status`'süz `From` doğrulama hatası verir.
- [x] Başka reponun görev ID'si ile `Update` ve `Delete` çağrısı `ErrNotFound` döner ve o satır değişmeden kalır.
- [x] En büyük ID'li görev silindikten sonra eklenen görev yeni bir ID alır (`AUTOINCREMENT`).

**Doğrulama:**
- [x] `go test -race ./internal/task/... ./internal/store/...`

**Bağımlılıklar:** Görev 2

**Dokunulacak dosyalar:**
- `internal/task/task.go`, `internal/task/task_test.go`
- `internal/store/store.go`, `internal/store/store_test.go`

**Kapsam:** M

### Görev 6: CLI `mv`, `edit` ve `rm`

**Açıklama:** `mv <id> <status> [--from status]`, `edit <id> [-t title] [-d description]` ve `rm <id>` komutlarını ekler. `mv` ve `edit` aynı `store.Update` metodunu çağırır. Domain hataları kullanıcıya okunur mesaj ve sıfırdan farklı exit code olarak döner.

**Kabul kriterleri:**
- [x] `agentboard mv 1 doing --from todo` görev `todo` durumunda değilse hata verir ve görevi değiştirmez.
- [x] `agentboard edit 1 -t "Yeni başlık"` sadece başlığı değiştirir; flag verilmezse hata verir.
- [x] `agentboard rm 1` görevi siler; olmayan veya sayı olmayan ID için anlaşılır bir hata verir.

**Doğrulama:**
- [x] `go test ./internal/cli/...`

**Bağımlılıklar:** Görev 4, Görev 5

**Dokunulacak dosyalar:**
- `internal/cli/update.go` (üç komut ortak `updateTask` ve `parseID` yardımcılarını paylaştığı için tek dosya)
- `internal/cli/cli_test.go`

**Kapsam:** S

### Kontrol noktası: CLI uçtan uca
- [x] `go build ./cmd/agentboard` ile derlenen binary, geçici bir `AGENTBOARD_HOME` ile şu akışı tamamlar: `add`, `ls`, `mv --from`, `edit`, `rm`.
- [x] Aynı reponun iki worktree'sinden çalıştırılan `ls` aynı listeyi gösterir.
- [ ] İnsan incelemesi, sonra Faz 3.

## Faz 3: MCP

### Görev 7: `serve`, `task_add` ve `task_list`

**Açıklama:** `mcpserver.New(store)` resmi go-sdk ile bir `*mcp.Server` kurar ve typed `mcp.AddTool` ile `task_add` ve `task_list` tool'larını kaydeder. Her tool zorunlu `cwd` parametresi alır ve repoyu her çağrıda `repo.Resolve(cwd)` ile çözer; göreli veya var olmayan yol tool hatası döner. Tool açıklamaları durumların anlamını ve ne zaman kullanılacağını içerir. `serve` komutu server'ı `mcp.StdioTransport` üzerinde çalıştırır ve stdout'a protokol dışında hiçbir şey yazmaz. Handler hatasının tool hatasına (`IsError`) nasıl eşlendiği SDK dokümantasyonundan doğrulanır.

**Kabul kriterleri:**
- [x] In-memory transport ile bağlanan client `ListTools` ile iki tool ve input şemalarını görür.
- [x] `task_add` çağrısı `#1 [backlog] ...` içeren metin döner; `task_list` varsayılan olarak `done` göstermez.
- [x] Geçersiz durum değeri ile göreli veya var olmayan `cwd` protokol hatası değil, `IsError: true` olan bir tool sonucu döner.

**Doğrulama:**
- [x] `go test ./internal/mcpserver/...` (`mcp.NewInMemoryTransports()` ile)

**Bağımlılıklar:** Görev 4

**Dokunulacak dosyalar:**
- `internal/mcpserver/server.go`
- `internal/mcpserver/server_test.go`
- `internal/cli/serve.go`

**Kapsam:** M

### Görev 8: `task_move`, `task_update` ve `task_delete`

**Açıklama:** Kalan üç tool'u ekler. `task_move` (`id`, `status`, `from?`) ve `task_update` (`id`, `title?`, `description?`) aynı `store.Update` metodunu çağırır. Agent için anlamları net kalsın diye ayrı tool'lardır (ISP). `task_move` opsiyonel `from` parametresi ile compare-and-swap yapar.

**Kabul kriterleri:**
- [x] `from` uyuşmazlığında sonuç `IsError: true` olur ve mesaj görevin mevcut durumunu içerir.
- [x] Repo A'nın `cwd`'si ile yapılan çağrı, repo B'nin görevini taşıyamaz, düzenleyemez ve silemez; aynı server iki repoya da doğru hizmet eder.
- [x] `task_update` sonrası `task_list` yeni başlığı gösterir; `task_delete` sonrası görevi göstermez.

**Doğrulama:**
- [x] `go test ./internal/mcpserver/...`

**Bağımlılıklar:** Görev 5, Görev 7

**Dokunulacak dosyalar:**
- `internal/mcpserver/server.go`
- `internal/mcpserver/server_test.go`

**Kapsam:** S

### Kontrol noktası: Gerçek agent entegrasyonu
- [ ] `go install ./cmd/agentboard` ve `claude mcp add --scope user agentboard -- agentboard serve` ile kurulum yapılır.
- [ ] Bir test reposunda açılan Claude Code oturumunda agent görev ekler ve `doing`'e taşır; `agentboard ls` aynı görevleri gösterir.
- [ ] İki paralel oturum aynı görevi `from=todo` ile `doing`'e çekmeye çalışır; biri başarılı olur, diğeri çakışma hatası alır.
- [ ] İnsan incelemesi, sonra Faz 4.

## Faz 4: Board

Faz 4 yalnızca Faz 2'ye bağlıdır; Faz 3 ile paralel yürütülebilir.

### Görev 9: Store `Repos` ve HTTP API

**Açıklama:** Store'a `Repos(ctx)` metodunu ekler (`SELECT repo, COUNT(*) ... GROUP BY repo`). `internal/httpapi` paketi `plan.md`'deki beş endpoint'i standart `ServeMux` pattern'leriyle sunar. `PATCH /api/tasks/{id}` gövdesi doğrudan `task.Patch`'e eşlenir ve `store.Update`'i çağırır; taşıma ve düzenleme aynı endpoint'ten geçer. Domain hataları tek bir fonksiyonda HTTP koduna eşlenir (`404`, `409`, `400`). Güvenlik middleware'i `Host` header'ını ve yazma isteklerindeki `Content-Type: application/json` zorunluluğunu uygular. Bu görevde statik dosya sunumu yoktur.

**Kabul kriterleri:**
- [x] `Host: evil.example` veya JSON olmayan bir `POST` isteği `403`/`415` ile reddedilir ve DB değişmez.
- [x] `PATCH /api/tasks/{id}` yanlış `from` ile `409` ve mevcut durumu, başka reponun görevi için `404`, sadece `title` içeren gövde için güncellenmiş görevi döner.
- [x] `GET /api/tasks` yanıtı `statuses` alanını `task` paketindeki sırayla içerir.

**Doğrulama:**
- [x] `go test -race ./internal/httpapi/... ./internal/store/...` (`httptest` ile)

**Bağımlılıklar:** Görev 5

**Dokunulacak dosyalar:**
- `internal/store/store.go`, `internal/store/store_test.go`
- `internal/httpapi/api.go`
- `internal/httpapi/api_test.go`

**Kapsam:** M

### Görev 10: `board` komutu ve embed

**Açıklama:** `web/embed.go` (paket `web`) `//go:embed all:dist` ile UI dosyalarını `fs.FS` olarak sunar; `web/dist/.gitkeep` commit edilir. `httpapi.New` UI dosyalarını `fs.FS` olarak alır ve `/api` dışındaki istekleri bu dosyalardan sunar; `index.html` yoksa build komutunu söyleyen düz bir sayfa döner. `board` komutu `--addr` (varsayılan `127.0.0.1:7420`) alır, loopback olmayan adresi reddeder, URL'yi `?repo=` ile yazar ve `Ctrl+C` ile düzgün kapanır.

**Kabul kriterleri:**
- [x] Node kurulu olmayan bir ortamda `go build ./...` ve `go test ./...` geçer.
- [x] UI build edilmemişken `/` bilgilendirici sayfa döner, `/api/repos` çalışır.
- [x] `agentboard board --addr 0.0.0.0:7420` hata verir ve server açılmaz.

**Doğrulama:**
- [x] `go test ./internal/httpapi/... ./internal/cli/...` (UI için `fstest.MapFS` ile)
- [x] Manuel: `agentboard board` çalıştırılır, `curl http://127.0.0.1:7420/api/repos` yanıt verir.

**Bağımlılıklar:** Görev 4, Görev 9

**Dokunulacak dosyalar:**
- `web/embed.go`, `web/dist/.gitkeep`, `.gitignore`
- `internal/httpapi/api.go`
- `internal/cli/board.go`

**Kapsam:** M

### Görev 11: Vite iskeleti ve salt okunur board

**Açıklama:** `web/` zaten Go embed paketini içerdiği için iskelet dışarıda `npm create vite@latest <tmp> -- --template vanilla-ts --no-interactive` ile üretilir; `package.json`, `tsconfig.json` (ek olarak `strict`) ve `index.html` demo dosyaları olmadan buna göre yazılır. `vite.config.ts` içinde `build.outDir: 'dist'` ve `/api` için `127.0.0.1:7420` proxy'si tanımlanır. `build` script'i `tsc && vite build && touch dist/.gitkeep` olur. Board repo seçiciyi (`?repo=` ile senkron), API'den gelen `statuses` sırasıyla 4 sütunu, sütun sayılarını ve kartları (`#id`, başlık, açıklamanın ilk satırı) gösterir. 2 saniyede bir yenilenir; sekme gizliyken durur.

**Kabul kriterleri:**
- [x] Seçicide DB'deki tüm repolar görünür; seçim değişince URL ve kartlar güncellenir, sayfa yenilenince seçim korunur.
- [x] CLI ile eklenen bir görev en geç 2 saniye içinde board'da görünür.
- [x] Frontend kodunda durum listesi sabit olarak tanımlı değildir; sütunlar API yanıtından üretilir.

**Doğrulama:**
- [x] `npm --prefix web ci && npm --prefix web run build` (tip kontrolü dahil) hatasız biter ve `web/dist/.gitkeep` yerinde kalır.
- [x] Manuel: `go build ./cmd/agentboard && ./agentboard board` sonrası tarayıcıda board açılır.

**Bağımlılıklar:** Görev 10

**Dokunulacak dosyalar:**
- `web/package.json`, `web/package-lock.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html` (iskelet)
- `web/src/main.ts`, `web/src/api.ts`, `web/src/style.css`

**Kapsam:** M (dosyaların çoğu iskelet tarafından üretilir)

### Görev 12: Sürükle-bırak ve çakışma yönetimi

**Açıklama:** Kartlar native HTML5 drag-and-drop ile sütunlar arasında taşınır; `PATCH` isteği `from` olarak kartın alındığı sütunu gönderir. `409` veya başka bir hata kısa bir mesajla gösterilir ve liste yenilenir. Sürükleme sırasında polling durur.

**Kabul kriterleri:**
- [x] Kart sürüklenip bırakıldığında durum DB'de değişir; `agentboard ls` yeni durumu gösterir.
- [x] Board açıkken CLI ile taşınmış bir kart, board'daki eski sütunundan sürüklenirse `409` mesajı görünür ve kart CLI'nin taşıdığı sütunda kalır.
- [x] Sürükleme sırasında polling kartın yerini değiştirmez.

**Doğrulama:**
- [x] `npm --prefix web run build`
- [x] Manuel: tarayıcıda yukarıdaki üç senaryo çalıştırılır.

**Bağımlılıklar:** Görev 11

**Dokunulacak dosyalar:**
- `web/src/main.ts`, `web/src/api.ts`, `web/src/style.css`

**Kapsam:** S

### Görev 13: Ekleme, düzenleme ve silme

**Açıklama:** Görev ekleme formu (başlık, açıklama; varsayılan durum `backlog`) eklenir. Karta tıklayınca başlık ve açıklama düzenlenebilir bir forma dönüşür; kaydetme `PATCH` ile yapılır. Kartta onaylı silme butonu bulunur. Düzenleme sırasında polling o kartın içeriğini ezmez.

**Kabul kriterleri:**
- [x] Eklenen görev `backlog` sütununda görünür.
- [x] Düzenlenen başlık ve açıklama kaydedilir; `agentboard ls` yeni başlığı gösterir. Boş başlık kaydedilemez ve hata mesajı görünür.
- [x] Silme onay ister; onaydan sonra kart kaybolur, iptal edilirse kart kalır.

**Doğrulama:**
- [x] `npm --prefix web run build`
- [x] Manuel: tarayıcıda yukarıdaki senaryolar çalıştırılır.

**Bağımlılıklar:** Görev 12

**Dokunulacak dosyalar:**
- `web/src/main.ts`, `web/src/api.ts`, `web/src/style.css`

**Kapsam:** S

### Kontrol noktası: Board uçtan uca
- [ ] MCP üzerinden agent'ın eklediği, taşıdığı ve düzenlediği görevler board'da en geç 2 saniye içinde görünür.
- [ ] İki farklı repodan eklenen görevler seçicide ayrı ayrı görünür ve birbirine karışmaz.
- [ ] İnsan incelemesi, sonra Faz 5.

## Faz 5: Dağıtım ve dokümantasyon

### Görev 14: CI, goreleaser, `--version` ve public repo

**Açıklama:** `.goreleaser.yaml` (`version: 2`) `before.hooks` içinde UI'yi build eder ve `./cmd/agentboard`'u `CGO_ENABLED=0` ile `darwin`/`linux`, `amd64`/`arm64` için derler. Sürüm `-ldflags` ile cobra root'un `Version` alanına verilir. `ci.yml` her push ve PR'da Go testlerini ve UI build'ini çalıştırır. `release.yml` `v*` tag'inde `goreleaser/goreleaser-action` ile GitHub Release oluşturur. Seçilen lisans `LICENSE` dosyası olarak eklenir. Public `yildizozan/agentboard` reposu kullanıcının açık onayından sonra oluşturulur ve ilk push yapılır.

**Kabul kriterleri:**
- [ ] `goreleaser check` config'i geçerli bulur.
- [ ] `goreleaser release --snapshot --clean` ile üretilen darwin/arm64 binary'si `board` komutunda UI'yi sunar ve `--version` sürümü yazar.
- [ ] Public repoya ilk push sonrası `ci.yml` yeşil biter.

**Doğrulama:**
- [ ] Yerel: `brew install goreleaser`, sonra `goreleaser check` ve `goreleaser release --snapshot --clean`
- [ ] GitHub: `gh run list` ile CI sonucu kontrol edilir.

**Bağımlılıklar:** Görev 1, Görev 11

**Dokunulacak dosyalar:**
- `.goreleaser.yaml`, `LICENSE`
- `.github/workflows/ci.yml`, `.github/workflows/release.yml`
- `cmd/agentboard/main.go`, `internal/cli/root.go`

**Kapsam:** M

### Görev 15: README, agent talimatı ve `AGENTS.md`

**Açıklama:** `README.md` kurulumu (GitHub Releases ve `go install`), Claude Code MCP yapılandırmasını, `board` komutunu, durumların anlamını, CLI kullanımını, `AGENTBOARD_HOME`'u ve bilinen sınırları (repo taşınınca liste kopar, oturum içi `cd` repoyu değiştirmez, `go install` ile kurulan binary'de UI yoktur) anlatır. Kullanıcıların kendi projelerindeki `AGENTS.md`/`CLAUDE.md` dosyasına ekleyeceği kısa bir agent talimat snippet'i içerir. Bu reponun `AGENTS.md` dosyası, Godot/GDScript referansları yerine Go ve Vite projesine uygun hale getirilir.

**Kabul kriterleri:**
- [ ] README'deki kurulum adımları temiz bir makinede takip edildiğinde MCP server ve board çalışır.
- [ ] Agent talimat snippet'i agent'a ne zaman görev ekleyeceğini, ne zaman `doing`'e çekeceğini (`from` ile), ne zaman görevi güncelleyeceğini ve ne zaman `done` yapacağını söyler.
- [ ] `AGENTS.md` içinde Godot, GDScript, sahne veya oyun kuralı referansı kalmaz.

**Doğrulama:**
- [ ] Manuel: README'deki komutlar sırayla çalıştırılır.

**Bağımlılıklar:** Görev 8, Görev 13, Görev 14

**Dokunulacak dosyalar:**
- `README.md`
- `AGENTS.md`

**Kapsam:** S

### Kontrol noktası: Tamamlandı
- [ ] Tüm kabul kriterleri karşılandı.
- [ ] `go test -race ./...` ve `npm --prefix web run build` geçer.
- [ ] İnceleme için hazır.
