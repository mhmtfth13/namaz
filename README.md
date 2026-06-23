# namaz

Terminalde `namaz` yaz → sıradaki namaz vaktine kalan süreyi gör. Windows, macOS, Linux.

```
$ namaz

  İkindi vaktine kalan 3 saat 19 dk  17:19
  Lokasyon: İstanbul
```

Vakitler [namazvakti.com](https://www.namazvakti.com) (Diyanet) kaynağından alınır. Tek binary, bağımlılık yok.

---

## Kurulum

Go kurmana gerek yok — hazır binary'yi [**Releases**](https://github.com/mhmtfth13/namaz/releases/latest) sayfasından indir, ya da aşağıdaki tek satırı çalıştır.

### macOS (Apple Silicon — M1/M2/M3)
```bash
curl -L https://github.com/mhmtfth13/namaz/releases/latest/download/namaz-macos-arm64 -o namaz
chmod +x namaz && sudo mv namaz /usr/local/bin/namaz
sudo xattr -d com.apple.quarantine /usr/local/bin/namaz   # "doğrulanamadı" uyarısını kaldırır
```

### macOS (Intel)
```bash
curl -L https://github.com/mhmtfth13/namaz/releases/latest/download/namaz-macos-intel -o namaz
chmod +x namaz && sudo mv namaz /usr/local/bin/namaz
sudo xattr -d com.apple.quarantine /usr/local/bin/namaz
```

### Linux
```bash
curl -L https://github.com/mhmtfth13/namaz/releases/latest/download/namaz-linux -o namaz
chmod +x namaz && sudo mv namaz /usr/local/bin/namaz
```

### Windows (PowerShell)
```powershell
mkdir $HOME\bin -Force
curl -L https://github.com/mhmtfth13/namaz/releases/latest/download/namaz.exe -o $HOME\bin\namaz.exe
# $HOME\bin'i PATH'e ekle (kalıcı), sonra terminali yeniden aç:
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";$HOME\bin", "User")
```

Kurulumu doğrula:
```bash
namaz --help
```

---

## Kullanım

```bash
namaz            # sıradaki vakit + kalan süre
namaz -a         # günün tüm vakitlerini listele
```

```
$ namaz -a

  İkindi vaktine kalan 3 saat 19 dk  17:19
  Lokasyon: İstanbul

  23 Haziran Salı
     İmsak   03:03
     Güneş   05:25
     Öğle    13:18
   › İkindi  17:19
     Akşam   20:48
     Yatsı   22:50
```

Renkler: **şu anki vakit yeşil** (`›` ile işaretli), **geçmiş vakitler kırmızı**, **gelecek vakitler gri**.

### Konum

Konum varsayılan olarak **IP'den otomatik** bulunur. VPN, mobil veri ya da kurumsal ağda yanlış şehir çıkabilir — o zaman sabitle:

```bash
namaz set "İstanbul"          # şehri sabitle (bir kez yeter, kaydedilir)
namaz set "Üsküdar"           # ilçe de olur
namaz --lat 41.02 --lon 29.02 # ya da ham koordinatla sabitle
namaz auto                    # otomatik (IP) konuma geri dön
namaz where                   # şu anki konum ayarını göster
```

Şehir adından koordinat çevirimi [Open-Meteo](https://open-meteo.com) ile yapılır.

### Diğer seçenekler

```bash
namaz --plain      # renksiz çıktı (NO_COLOR ortam değişkeni de çalışır)
namaz --no-cache   # önbelleği atla, taze çek
namaz --help       # yardım
```

---

## Nasıl çalışıyor

```
namaz
   │
   ├─ Konum çöz ──── config "fixed" mı? → kayıtlı lat/lon
   │                 değilse           → IP'den (ip-api.com)
   │
   ├─ Cache kontrol ─ bugünün + aynı konumun vakitleri diskte mi?
   │                  varsa → anında oku (offline)
   │                  yoksa → namazvakti.com/Main.php'den çek + kaydet
   │
   └─ Hesapla ─────── her vaktin unix timestamp'i ile şimdiyi karşılaştır
                      ilk gelecek vakit = sıradaki, fark = kalan süre
```

- **Günde tek internet.** İlk çağrı siteye gider (~1 sn), aynı günün geri kalan tüm çağrıları diskten okunur — **offline çalışır**.
- **Saat dilimi derdi yok.** Site her vakti mutlak unix timestamp olarak veriyor; sadece "şimdi" ile karşılaştırılıyor, tarih/tz matematiği yok.
- **Konum değişince** (`namaz set ...`) cache otomatik temizlenir; yanlış şehrin vakti gösterilmez.
- **Yatsı sonrası** (yatsı–gece yarısı arası) bugünün tüm vakitleri geçtiğinde, yarının İmsak'ı aylık tablodan çekilip `(yarın)` etiketiyle gösterilir.

### Dosya konumları

| Ne          | Yer                                                        |
|-------------|------------------------------------------------------------|
| Konum ayarı | `~/.config/namaz/config.json` (Win: `%AppData%\namaz\`)    |
| Günlük cache| `~/.config/namaz/today.json`                               |

---

## Kaynaktan derleme

[Go](https://go.dev) 1.21+ gerekir.

```bash
go build -o namaz .                  # bu sistem için
```

Üç platform için tek seferde:
```bash
GOOS=darwin  GOARCH=arm64 go build -o dist/namaz-macos-arm64 .
GOOS=darwin  GOARCH=amd64 go build -o dist/namaz-macos-intel .
GOOS=linux   GOARCH=amd64 go build -o dist/namaz-linux .
GOOS=windows GOARCH=amd64 go build -o dist/namaz.exe .
```

---

## Sınırlar

- **IP konumu** datacenter/VPN IP'sinde yanlış olur. Çözüm: `namaz set` ile sabitle.
- **namazvakti.com'a bağımlı.** Site HTML yapısını (`vakitts` attribute) değiştirirse parse kırılır; "vakitler ayrıştırılamadı" hatası verir.
- Vakit hesabını site yapar; bu araç sadece gösterir.

## Lisans

MIT
