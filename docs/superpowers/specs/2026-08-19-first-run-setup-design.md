# First-Run Setup — Design

**Date:** 2026-08-19
**Status:** approved

## Problem

Instance Ku-CRUD baru tidak punya user. Satu-satunya cara membuat user pertama
adalah CLI `seed-admin`. UX-nya buruk: user buka app → halaman login → tidak
bisa masuk → harus baca README.

## Solution

Halaman `/setup` publik + dua endpoint tanpa auth yang hanya berguna (dan untuk
POST: hanya bisa) selama belum ada user.

## Backend — `internal/api/setup.go`

- `GET /api/setup/status` → `{"needed":true|false}`; `needed` = tabel users
  kosong. Publik dengan sengaja; hanya membocorkan apakah instance sudah
  memiliki user.
- `POST /api/setup` body `{username,password}`:
  - Validasi: username non-kosong, password ≥ 4 karakter → 400 `VALIDATION`.
  - Insert **atomic**, tanpa race window:
    `INSERT INTO users(username,password_hash) SELECT ?,? WHERE NOT EXISTS(SELECT 1 FROM users)`
  - 0 row affected → 403 `AUTH` "setup already completed".
  - Sukses → 200 `{"ok":true}`. Tidak set session cookie — user login manual
    (keputusan pemilik produk).
- Kedua route didaftarkan TANPA `RequireAuth` di `Routes()`.
- bcrypt hashing via `meta.Store.CreateUser` tidak dipakai di path ini;
  handler memanggil method store baru `CreateFirstUser(username, password) (bool, error)`
  yang mengeksekusi INSERT-conditional di atas (bool = created).

## Frontend

- `web/src/pages/Setup.tsx` — route publik `/setup` di main.tsx (di luar layout
  App, seperti `/login`). Form username+password tanpa confirm-field. On mount
  panggil status; jika `needed:false` → redirect `/login`. Submit → POST setup →
  sukses redirect `/login` (dengan pesan kecil "user created, please login").
- `Login.tsx` — on mount cek status; jika `needed:true` → redirect `/setup`.
- Alur lengkap: buka app → 401 → login → (belum ada user) → `/setup` → buat
  user → login.

## Invariants

- POST /api/setup gagal permanen setelah user pertama ada — tidak ada jalan
  HTTP untuk membuat user kedua.
- `seed-admin` CLI tetap berfungsi; instance yang di-seed via CLI tidak pernah
  menawarkan /setup (status needed:false).

## Testing — `internal/api/setup_test.go`

1. DB kosong → status `needed:true`.
2. POST setup sukses (200) → status `needed:false` → login dengan kredensial
   baru berhasil (200 + cookie).
3. POST setup kedua → 403.
4. Password < 4 char / username kosong → 400 VALIDATION.
5. Route tanpa cookie sama sekali (bukan 401) — endpoints memang publik.

Frontend: `npm run build` hijau cukup (tidak ada test framework web).

## Out of scope

- Confirm-password field, email, multi-user management UI, invite flow.
