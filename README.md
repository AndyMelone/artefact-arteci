# ARTECI — API de normalisation de dates CSV/Excel

API haute performance pour normaliser des colonnes de dates dans des fichiers CSV/Excel stockés dans MinIO.

## Endpoints

| Méthode | Route | Description |
|---------|-------|-------------|
| `GET` | `/columns?bucket=<bucket>&file=<nom>` | Liste les colonnes du fichier |
| `POST` | `/processDate` | Normalise les dates en place, retourne les 100 premières lignes |
| `GET` | `/health` | Health check |
| `GET` | `/docs` | Documentation Swagger interactive |

### GET /columns

```bash
curl "http://localhost:3001/columns?bucket=raw&file=lst_of_users_anon_1.csv"
```

### POST /processDate — body

```json
{
  "bucket": "raw",
  "file": "lst_of_users_anon_1.csv",
  "date_columns": ["DATE_CREATION", "DATE_DESACTIVATION", "DATE_DERNIERE_CONNECTION_1"],
  "date_formats": ["MDY", "MDY", "MDY"]
}
```

**`date_formats`** : `MDY` (mois/jour/année — en_US) ou `DMY` (jour/mois/année — fr_FR). Résout les ambiguïtés pour les jours ≤ 12.

**Output** : `DD-MM-YYYY HH:mm:ss` (heure `00:00:00` si absente de la source).

**Écriture en place** : le fichier est modifié directement dans le bucket indiqué (`bucket`), au même chemin (`file`). Aucun bucket de destination séparé.

---

## Démarrage rapide

### Option A — Sans Docker (Go uniquement)

Prérequis : Go 1.22+, une instance MinIO accessible.

```bash
# Démarrer MinIO en local si besoin
docker run -d -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
  minio/minio server /data --console-address :9001

# Lancer l'API Go
cd go
go run .
```

L'API démarre sur `:3001`. Les variables sont configurables via `go/.env` (copier depuis `.env.example`).

### Option B — Avec Docker Compose (API + MinIO)

```bash
docker compose -f docker/docker-compose.yml up -d --build
```

- API : `http://localhost:3001`
- MinIO Console : `http://localhost:9001` (minioadmin / minioadmin)

Le bucket `arteci` est créé automatiquement. Les fixtures (`fixtures/*.csv`) sont uploadées si les fichiers de production sont absents.

### Option C — Kubernetes (k3s / tout cluster K8s)

```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/go/api-secret.yaml
kubectl apply -f k8s/go/api-configmap.yaml
kubectl apply -f k8s/minio/minio-deployment.yaml
kubectl apply -f k8s/minio/minio-service.yaml
kubectl apply -f k8s/minio/minio-init-job.yaml
kubectl apply -f k8s/go/api-deployment.yaml
kubectl apply -f k8s/go/api-service.yaml
```

Tester localement avec Vagrant + k3s (Apple Silicon ou x86) :

```bash
cd Vagrant && vagrant up
export KUBECONFIG=$(pwd)/kubeconfig.yaml
kubectl get pods -n arteci
```

---

### Uploader un fichier et appeler l'API

```bash
# Uploader un fichier dans MinIO
mc alias set local http://localhost:9000 minioadmin minioadmin
mc cp ressources/lst_of_users_anon_1.csv local/arteci/

# Lister les colonnes
curl "http://localhost:3001/columns?bucket=arteci&file=lst_of_users_anon_1.csv"

# Normaliser les dates (écriture en place dans le bucket)
curl -X POST http://localhost:3001/processDate \
  -H "Content-Type: application/json" \
  -d '{
    "bucket": "arteci",
    "file": "lst_of_users_anon_1.csv",
    "date_columns": ["DATE_CREATION", "DATE_DESACTIVATION", "DATE_DERNIERE_CONNECTION_1"],
    "date_formats": ["MDY", "MDY", "MDY"]
  }'
```

---

## Formats de dates supportés

| Groupe | Exemples |
|--------|----------|
| ISO 8601 | `2024-03-15`, `2024-03-15T14:30:00Z`, `2024-03-15T14:30:00.123` |
| Timestamp Unix | `1710460800` (10 chiffres = secondes), `1710460800000` (13 = ms) |
| en_US (MDY) | `03/15/2024`, `3/5/2024 14:30`, `Mar 15, 2024`, `March 15, 2024` |
| fr_FR (DMY) | `15/03/2024`, `15/03/2024 14:30:00`, `15 mars 2024` |

Source : Qlik Talend Data Preparation 8.0.

Cellule vide ou valeur non parseable → retournée telle quelle, sans erreur.

---

## Flux streaming (mémoire-efficient)

```
MinIO <bucket>/<file> → getObject() → Readable stream
  │
  ├─ [CSV]  csv-parse (delimiter=';')
  │           → DateTransformStream (normalize + collect first 100 rows)
  │           → csv-stringify
  │           → MinIO <bucket>/<file>  ← putObject en place (même chemin)
  │
  └─ [XLSX] ExcelJS WorkbookReader (streaming)
              → DateTransformStream
              → ExcelJS WorkbookWriter
              → MinIO <bucket>/<file>  ← putObject en place
```

Jamais plus de 100 rows en mémoire simultanément (hors pipeline Node.js).  
`putObject` sans taille déclarée → multipart upload automatique MinIO.

---

## Observabilité — OTel → SigNoz

Spans instrumentés :
- `minio.getObject` — attributs : bucket, file, file_size_bytes
- `processDate.csv` / `processDate.excel` — attributs : bucket, file, columns, total_rows, rows_failed
- `minio.putObject` — attributs : bucket, file, duration_ms
- HTTP in/out via instrumentations automatiques (`@opentelemetry/instrumentation-http`)

Logs structurés via `@opentelemetry/api-logs` avec `traceId` + `spanId` corrélés.

---

## Gestion d'erreurs

| Cas | HTTP | Message |
|-----|------|---------|
| Fichier introuvable | 404 | `File 'path' not found in bucket 'raw'` |
| Colonne inexistante | 422 | `Column 'X' not found in file. Available: A, B, C` |
| Bucket inexistant | 404 | `Bucket 'raw' does not exist in MinIO` |
| Longueurs incohérentes | 400 | `date_columns (3 items) and date_formats (2 items) must have the same length` |
| Type de fichier non supporté | 422 | `Unsupported file type '.xyz'. Supported: csv, xlsx` |
| Format invalide | 400 | validé par class-validator (MDY ou DMY uniquement) |

---

## Tests

```bash
cd api
npm install
npm test
npm run test:cov  # couverture
```

---

## Variables d'environnement

| Variable | Défaut | Description |
|----------|--------|-------------|
| `PORT` | `3000` | Port d'écoute de l'API |
| `MINIO_ENDPOINT` | `localhost` | Hostname MinIO |
| `MINIO_PORT` | `9000` | Port MinIO |
| `MINIO_USE_SSL` | `false` | TLS MinIO |
| `MINIO_ACCESS_KEY` | `minioadmin` | Clé d'accès |
| `MINIO_SECRET_KEY` | `minioadmin` | Clé secrète |
| `MINIO_BUCKET` | `raw` | Bucket créé automatiquement au démarrage |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4317` | Endpoint OTLP gRPC SigNoz |
| `OTEL_SERVICE_NAME` | `arteci-api` | Nom du service dans SigNoz |

---

## Benchmarks (fichiers de test)

| Fichier | Taille | Lignes | Target | Résultat |
|---------|--------|--------|--------|----------|
| lst_of_users_anon_1.csv | 28 MB | 320K | ≤ 20s | _à mesurer_ |
| lst_of_users_anon_2.csv | 182 MB | 2.1M | ≤ 50s | _à mesurer_ |
| lst_of_users_anon_3.csv | 931 MB | 10.8M | ≤ 2min | _à mesurer_ |

> Mesurés sur MacBook Pro M2 / Docker Desktop 4.x. Renseigner après validation end-to-end.

---

## CI/CD — GitHub Actions

Pipeline `.github/workflows/ci.yml` :
1. `go vet` → `go build` → `go test` (tests unitaires date-parser)
2. Build Docker image multi-stage (linux/amd64 + linux/arm64)
3. Push sur Docker Hub (branche `main` uniquement)

**Secrets à configurer dans le repo GitHub :**
- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`

---

## Compromis documentés

| Décision | Raison | Impact |
|----------|--------|--------|
| NestJS/TypeScript vs Go | TypeScript = velocity P0, Go = bonus post-livraison | Port Go possible sans refactor (date-parser sans couplage NestJS) |
| `date-fns` vs `luxon` / `dayjs` | Léger, tree-shakeable, zéro dépendances, `parse()` format-explicit | Pas de magie, comportement prévisible sur ambiguïtés |
| csv-parse streaming vs readFile | Mémoire constante quelle que soit la taille du fichier | Légère complexité de pipeline Node.js Transform |
| ExcelJS WorkbookReader streaming | Seule lib Node.js avec vrai streaming XLSX | Nécessite buffer complet pour fichiers >500MB avec certains encodages |
| Écriture en place (même bucket, même chemin) | Conforme au cahier des charges | L'original est remplacé — pas de gestion de versioning |
| SigNoz dans docker-compose.yml | Un seul fichier pour démarrer toute la stack | Démarrage ~3-4 min (ClickHouse Keeper → migrator → ingester) |
