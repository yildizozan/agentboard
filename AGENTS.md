## Kod tasarımı

- **SOLID:** Paket, tip ve fonksiyonların tek bir değişim nedeni olsun (SRP). Gerçek genişleme ihtiyacında davranışı mevcut kodu dağıtmadan genişlet (OCP). Alt türler üst türün beklenen davranışını korusun (LSP). Çağıranları kullanmadıkları geniş sözleşmelere bağımlı kılma (ISP). Yüksek seviyeli domain kurallarını somut altyapı ayrıntılarına gereksiz yere bağlama (DIP). Bu ilkeler için sırf biçimsel uyum adına yeni katmanlar oluşturma.
- **DRY:** Aynı iş kuralı veya bilgiyi birden çok yerde tutma; ortak davranışı tek kaynağa taşı. Yalnızca görünüşte benzer, farklı nedenlerle değişen kodu zorla birleştirme.
- **KISS:** Doğrudan okunabilen en basit çözümü seç. Açık isimler ve kısa, odaklı fonksiyonlar kullan; gereksiz dolaylılık ve durum yönetiminden kaçın.
- **YAGNI:** Bugünkü gereksinim için gerekmeyen özellik, ayar veya soyutlamayı ekleme. Gereken yeniden düzenlemeyi ve testleri erteleme.

## Karmaşıklık ve yeniden kullanım

- **Tek sorumluluk:** Taşıma katmanları (CLI, MCP, HTTP), domain kuralları, kalıcılık ve sunum gibi farklı değişim nedenlerini uygun paket sınırlarında ayır. Bir fonksiyonun ne yaptığını adıyla açıklayabil.
- **Bilişsel karmaşıklık:** İç içe koşul ve döngüler okuyucunun takip yükünü artırır. Koruyucu koşullar, açık ara adımlar ve iyi adlandırılmış yardımcı fonksiyonlarla akışı sadeleştir; karmaşıklığı yalnızca başka fonksiyona taşıma.
- **Döngüsel karmaşıklık:** Bağımsız karar yollarını azalt; anlamlı dalları test et. Metrikleri inceleme sinyali olarak kullan, bağlamdan bağımsız sayısal hedef uğruna okunabilirliği bozma.
- **Yeniden kullanılabilirlik:** Tekrarlanan gerçek davranışı odaklı paketler ve fonksiyonlarla paylaş. Bağımlılıkları açık tut; yapılandırmayı dışarıdan ver. Henüz tek kullanımlık kod için genel çatı kurma.
- Değişen davranışı ilgili testlerle doğrula; Go (`gofmt`, Effective Go) ve TypeScript adlandırma/biçim düzenini izle.
