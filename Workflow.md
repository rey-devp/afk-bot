# Dokumentasi Alur Kerja (Workflow): Discord 24/7 AFK Bot

## 1. Product Requirements Document (PRD)

**1.1. Tujuan Produk**
Mengembangkan bot Discord yang sangat ringan dan efisien dengan tugas tunggal: bergabung ke dalam sebuah *Voice Channel* (VC) spesifik dan bertahan di sana selama 24/7 tanpa terputus (AFK).

**1.2. Target Pengguna**
Administrator atau pemilik server Discord yang membutuhkan kehadiran bot di *Voice Channel* secara permanen untuk tujuan estetika server, menjaga VC tetap aktif, atau fungsi pengawasan pasif.

**1.3. Fitur Utama**
*   **Auto-Join:** Bot otomatis masuk ke *Voice Channel* yang telah ditentukan saat server dinyalakan.
*   **Anti-Disconnect (Silence Broadcaster):** Bot memancarkan paket audio kosong (*silence frames*) secara berkala untuk memanipulasi fitur penghematan *bandwidth* Discord sehingga bot tidak ditendang dari *room*.
*   **Self-Deafen:** Bot menonaktifkan pendengarannya (*deafen*) untuk menghemat memori dan bandwidth karena tidak perlu memproses suara masuk.
*   **Health Check Endpoint:** Terdapat *web server* HTTP sederhana yang merespons ping dari layanan *hosting* agar bot tetap dibiarkan hidup.

## 2. Software Requirements Specification (SRS)

**2.1. Kebutuhan Fungsional**
*   Sistem harus dapat membaca *Environment Variables* untuk konfigurasi rahasia: `BOT_TOKEN`, `GUILD_ID`, `VOICE_CHANNEL_ID`, dan `PORT`.
*   Sistem harus merespons dengan status HTTP 200 OK pada *endpoint* `/` untuk keperluan *health check*.
*   Sistem harus melakukan otentikasi ke API Discord melalui *gateway* menggunakan koneksi *WebSocket*.
*   Sistem harus memutar *buffer* audio Opus yang merepresentasikan keheningan (*silence*) ke dalam *voice stream*.

**2.2. Kebutuhan Non-Fungsional**
*   **Bahasa Pemrograman:** Golang (dipilih karena penggunaan RAM yang sangat kecil dan efisiensi CPU).
*   **Library Utama:** `discordgo` (untuk interaksi API Discord).
*   **Performa:** Aplikasi harus menggunakan RAM di bawah 50MB.
*   **Ketersediaan (Uptime):** Mendukung 99.9% uptime dengan penanganan koneksi ulang otomatis (auto-reconnect) yang disediakan oleh `discordgo`.

## 3. Software Design Document (SDD)

**3.1. Arsitektur Sistem**
Sistem menggunakan arsitektur monolitik ringan (micro-service) yang terdiri dari dua proses asinkron utama (*Goroutines*):
1.  **HTTP Server Goroutine:** Menangani *request* masuk di *port* yang ditentukan.
2.  **Discord Session Goroutine:** Menjaga koneksi *WebSocket* Discord dan transmisi UDP untuk *Voice Channel*.

**3.2. Struktur Direktori Proyek**
```text
/bot-afk
├── main.go           # Entry point: inisialisasi HTTP server & bot
├── audio.go          # Logika untuk men-generate dan mengirim Opus silence frame
├── go.mod            # Golang module dependencies
├── go.sum            # Checksums dependensi
└── Dockerfile        # Konfigurasi containerization
```

**3.3. Infrastruktur & Deployment**
*   **Version Control:** Menggunakan **GitHub** untuk menyimpan *source code* secara aman dan kolaboratif.
*   **Containerization:** Aplikasi akan dibungkus menggunakan **Docker** (*Multi-stage build*) untuk menghasilkan *image* berukuran sangat kecil.
*   **Hosting/Deployment:** *Deployment* akan dilakukan ke **Railway**, yang secara otomatis akan menarik kode dari GitHub, membangun Docker image, dan menggunakan HTTP *endpoint* sebagai *health check* keberlangsungan aplikasi.

## 4. Task Breakdown (Work Breakdown Structure)

Manajemen tugas ini dapat diimplementasikan ke dalam papan Kanban pada **Trello** untuk pelacakan progres.

**Fase 1: Inisialisasi Proyek**
*   [ ] Membuat repositori Git baru di **GitHub**.
*   [ ] Menjalankan `go mod init` untuk inisialisasi modul Golang.
*   [ ] Mengunduh dependensi utama: `go get github.com/bwmarrin/discordgo`.
*   [ ] Membuat *bot application* di Discord Developer Portal dan mendapatkan `BOT_TOKEN`.

**Fase 2: Pengembangan Inti (Development)**
*   [ ] Mengimplementasikan HTTP Server ringan menggunakan `net/http` pada `main.go`.
*   [ ] Menguji coba *endpoint* menggunakan **Postman** untuk memastikan respons web server aktif.
*   [ ] Mengonfigurasi `discordgo.Session` dan menambahkan *Event Handler* untuk *event* `Ready`.
*   [ ] Mengimplementasikan fungsi `ChannelVoiceJoin(GuildID, ChannelID, false, true)` agar bot masuk dalam keadaan *deafened*.
*   [ ] Menulis fungsi *audio sender* yang terus-menerus mengirimkan *Opus frames* kosong ke saluran UDP `voiceConnection`.
*   [ ] Menambahkan logika *graceful shutdown* menggunakan `os/signal` agar bot *disconnect* dengan rapi saat dimatikan.

**Fase 3: Containerization & Persiapan Rilis**
*   [ ] Menulis `Dockerfile` menggunakan *multi-stage build* (meng-compile aplikasi di *stage* pertama, dan memindahkan *binary*-nya ke *stage* produksi berbasis alpine).
*   [ ] Melakukan *build* dan *testing* Docker *image* secara lokal.

**Fase 4: Deployment ke Lingkungan Produksi**
*   [ ] Menghubungkan repositori GitHub dengan proyek aplikasi di **Railway**.
*   [ ] Mengonfigurasi *Environment Variables* di *dashboard* Railway (`BOT_TOKEN`, `GUILD_ID`, `VOICE_CHANNEL_ID`).
*   [ ] Melakukan *deploy* pertama dan memantau metrik performa serta log aplikasi.
