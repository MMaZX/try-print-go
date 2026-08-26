# Deferred Work

## Deferred from: code review of 5-2-reintentos-controlados-en-fallos-de-impresion (2026-08-21)

- **Carrera Upsert de reimpresión vs worker en vuelo** [usqay-print-client/internal/ws/connection.go:271, usqay-print-client/internal/queue/repository.go:77] — `INSERT OR REPLACE` reinserta la fila (PENDING, intentos=0) mientras el worker tiene una copia en vuelo; el `finalize` posterior ejecuta `UPDATE ... WHERE id = ?` sin guard de estado y marca PRINTED una reimpresión nunca impresa. Preexistente a esta story; requiere guard condicional (`WHERE estado = 'PROCESSING'`) o columna de versión.
- **Barrido de jobs PROCESSING al arrancar** [usqay-print-client/cmd/client/main.go:103] — un crash entre `UpdateStatus(PROCESSING)` y cualquier escritura terminal congela el job para siempre (ni reimpreso ni errado). Preexistente; candidato natural para la story 5.3 (test e2e caída/reconexión) o historia dedicada de recuperación.
- **Backoff con reloj de pared persistido** [usqay-print-client/internal/queue/repository.go:95, usqay-print-client/internal/queue/worker.go:166] — un salto de reloj (NTP, suspend/resume en la VM Windows) pospone o acelera masivamente los reintentos. Mitigación posible: acotar `next_retry_at` al arrancar.
- **Matching frágil por texto "duplicate column name"** [usqay-print-client/internal/queue/sqlite.go:65,73,79] — las 3 migraciones dependen del texto exacto del error de modernc.org/sqlite; centralizar en helper con detección por código de error.
