# Ku-CRUD v1 — Design Spec

Tanggal: 2026-08-19
Status: Approved (via brainstorming session)

## 1. Ringkasan

Ku-CRUD adalah aplikasi CRUD runtime-dynamic: seluruh aksi (definisi datasource, pemilihan tabel, konfigurasi field) dilakukan setelah build & deploy. Terinspirasi GroceryCrud, dibawa ke runtime. Target user: orang yang malas mengedit data langsung dari database client.

Stack: **Go (backend) + React (frontend)**, dikirim sebagai **satu binary** (frontend di-embed via `go:embed`). Deployment = copy 1 file, jalankan.

## 2. Keputusan Desain (hasil brainstorming)

| Keputusan | Pilihan |
|---|---|
| Deployment | Single binary (go:embed) |
| Schema drift | Cek saat halaman tabel dikunjungi saja, bukan per aksi CRUD |
| Audit trail | Full diff (old/new values sebagai JSON) |
| Kredensial datasource | Plaintext di SQLite + dokumentasi risiko |
| Arsitektur | Monolitik bersih, tanpa abstraksi Driver di v1 |
| Frontend | React + Vite + Tailwind + **shadcn/ui**, TanStack Query + TanStack Table |

## 3. Arsitektur

```
ku-crud/
  cmd/ku-crud/main.go    # flags: -addr, -data (path sqlite); serve API + SPA
  internal/api/          # handler HTTP, middleware auth
  internal/meta/         # SQLite: migrasi, definisi, user, audit
  internal/ds/           # Postgres: introspeksi schema, query builder, eksekusi
  web/                   # React (Vite); build → di-embed go:embed
```

- Go 1.22+ `net/http` ServeMux (method pattern) — tanpa router library.
- SQLite via `modernc.org/sqlite` (pure Go, no CGO → cross-compile mudah).
- Postgres via `jackc/pgx` (mode `database/sql`).
- Frontend: React + Vite + Tailwind + shadcn/ui + TanStack Query + TanStack Table.

## 4. Skema Metadata (SQLite)

```sql
users(id, username UNIQUE, password_hash, created_at)
datasources(id, name UNIQUE, host, port, dbname, username, password, sslmode)
table_defs(id, datasource_id, schema_name, table_name, label,
           pk_column, searchable_cols, sortable_cols, page_size, created_at)
columns(id, table_def_id, name, label,
        field_type,      -- boolean|text|number|datetime|enum
        enum_options,    -- JSON array, null jika bukan enum
        editable, required, visible, position)
audit(id, user_id, table_def_id, action,  -- INSERT|UPDATE|DELETE
      row_pk, old_values, new_values,     -- JSON; old_values null utk INSERT,
                                          -- new_values null utk DELETE
      created_at)
settings(key PRIMARY KEY, value)  -- menyimpan session secret, dll.
```

## 5. Auth

- Login sederhana, user disimpan di SQLite (bcrypt). Tidak ada hak akses/roles — semua yang login bisa segalanya (sesuai brief: hanya proteksi).
- Session: signed cookie (HMAC), secret di-generate sekali saat first run, disimpan di tabel `settings`. Expiry 24 jam.
- User dibuat via CLI flag `-create-user <username>` (prompt password via stdin). Tidak ada first-run wizard di frontend.

## 6. Definisi Datasource

- Form satu flow: `name, host, port, dbname (WAJIB), username, password, sslmode`.
- `dbname` wajib dan melekat pada datasource — **tidak ada fitur list-all-databases**. Ganti database = buat datasource baru.
- Tombol **Test Connection** → connect + `SELECT 1` ke DB tersebut, lapor sukses/gagal + pesan error mentah Postgres.

## 7. Definisi Tabel (wizard 3 langkah)

1. **Pilih datasource** (dropdown dari daftar datasource).
2. **Pilih tabel** (dropdown dari `information_schema.tables` datasource terpilih).
3. **Konfigurasi kolom** — auto-deteksi dari `information_schema.columns`, ditampilkan sebagai grid editor dengan default terisi. User override per kolom:
   - **PK** — tepat satu kolom wajib dipilih sebelum simpan (identitas baris untuk UPDATE/DELETE). Pre-select dari PK asli Postgres bila ada.
   - **Data type** — boolean / text / number / datetime / enum (+ isi opsi enum).
   - **Is editable**, **Is visible** (hidden = tidak muncul di grid & form).
   - **Label**, **Required**.
   - Searchable/sortable per tabel + page size (dari brief poin 6) dikonfigurasi di langkah ini juga.

### Mapping tipe PG → field type (auto, bisa di-override user)

| PG type | Field type |
|---|---|
| `bool` | boolean |
| `int2/4/8`, `numeric`, `float*` | number |
| `timestamp*`, `date`, `time*` | datetime |
| `text`, `varchar`, `char` | text |
| native enum (`pg_enum`) | enum (opsi dari `pg_enum`) |
| lainnya (array, json, uuid, bytea, ...) | **exclude** (tidak masuk definisi di v1) |

User boleh override text → enum dengan mengisi opsi sendiri.

### Aturan PK

- Tabel tanpa PK asli tetap bisa didefinisikan — user wajib pilih satu kolom sebagai PK.
- Tidak ada validasi uniqueness pilihan user (scan unique mahal di tabel besar). Satu UPDATE/DELETE bisa mengenai >1 baris jika pilihan tidak unique; rowcount yang terkena dicatat di audit. `// ponytail: trust user PK pick`

## 8. Halaman CRUD Grid

- **READ**: grid dengan searching (ILIKE pada `searchable_cols`), sorting (`sortable_cols`), pagination (`page_size`).
- **CREATE/UPDATE**: form dialog, hanya field `editable`. Validasi: `required`, tipe (number, datetime ISO 8601 via input `datetime-local`, enum harus salah satu opsi), boolean toggle.
- **DELETE**: konfirmasi dialog.
- Query list: `SELECT ... WHERE ... ILIKE $n ... ORDER BY "col" ASC|DESC LIMIT $n OFFSET $n` + query count terpisah.

## 9. Schema Drift

- Drift check **hanya saat halaman tabel dikunjungi** (satu endpoint verify saat load): bandingkan kolom terdefinisi vs `information_schema` live.
- Match → lanjut normal. Mismatch → banner di frontend + tombol **Re-sync** (hapus kolom hilang dari definisi, tambah kolom baru dengan default mapping, pertahankan label/setting kolom yang sama).
- Setelah halaman dibuka, aksi CRUD memakai definisi tersimpan tanpa cek ulang.
- Keamanan identifier tidak bergantung pada drift check: nama tabel/kolom di definisi hanya lahir dari introspeksi (bukan input bebas), identifier selalu di-quote, sort/search dari frontend divalidasi terhadap definisi tersimpan, value selalu parameterized `$n`.

## 10. Audit Trail (full diff)

- Setiap INSERT/UPDATE/DELETE mencatat: user, waktu, tabel, aksi, `row_pk`, `old_values` + `new_values` (JSON).
- Untuk UPDATE/DELETE: fetch old row(s) dulu → aksi → tulis **satu entry audit per baris yang terkena** (menutup kasus PK tidak unique: `row_pk` tiap baris tetap tercatat akurat).
- Audit di SQLite, beda koneksi dengan Postgres → bukan satu transaksi. Jika tulis audit gagal: aksi tetap sukses, error di-log server. `// ponytail: best-effort audit, outbox pattern kalau perlu guarantee`
- Halaman read-only untuk melihat audit log (filter per tabel/user/aksi).

## 11. Keamanan

- **Kredensial datasource plaintext di SQLite.** Didokumentasikan di README: siapa pun yang bisa membaca file SQLite bisa membaca password. Mitigasi adalah tanggung jawab operator (file permission).
- SQL injection: identifier di-quote ketat dan hanya berasal dari introspeksi/definisi tervalida; value selalu parameterized.
- Password user: bcrypt.
- Session cookie: HttpOnly, SameSite=Lax, signed HMAC.

## 12. Error Handling

JSON error konsisten: `{code, message, detail}`. Kode:

| Code | HTTP | Makna |
|---|---|---|
| `AUTH` | 401 | belum login / session expired |
| `CONN` | 502 | koneksi Postgres datasource gagal |
| `DRIFT` | 409 | definisi tidak cocok dengan schema live |
| `NOT_FOUND` | 404 | resource tidak ada |
| `VALIDATION` | 400 | input tidak valid |

Pesan error Postgres mentah diteruskan (dibungkus) untuk membantu debugging user.

## 13. Testing

- **Unit test Go (wajib)**: query builder (quoting, urutan param, search/sort/pagination), type mapping, drift comparator.
- **Integration test**: satu suite terhadap Postgres asli (docker compose untuk dev/CI).
- Frontend: manual di v1, tanpa test.

## 14. Non-goals v1 (eksplisit)

- DDL / ALTER table (tanggung jawab penuh user)
- Hak akses / roles
- Target DB selain Postgres
- List-all-databases dalam satu datasource
- Validasi uniqueness PK pilihan user
- Export CSV, dark mode, i18n
- Test otomatis frontend

UI berbahasa Inggris (standar dev tool); i18n bisa ditambah nanti.

## 15. Risalah Keputusan Singkat

- `modernc.org/sqlite` dipilih agar CGO-free → single binary cross-platform tanpa toolchain C.
- Tanpa interface `Driver` multi-DB di v1: satu implementasi = YAGNI; refactor saat v2 lebih murah setelah API stabil.
- Plaintext kredensial: kesadaran penuh user, internal tool, mitigasi via file permission.
- Drift check per-kunjungan-halaman: keseimbangan antara keamanan data dan query ekstra per aksi.
