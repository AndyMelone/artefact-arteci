# ARTECI — API de normalisation de dates CSV/Excel

API haute performance pour normaliser des colonnes de dates dans des fichiers CSV/Excel stockés dans MinIO.

## Démo UI

Une interface web minimaliste est intégrée à l'API Go et accessible directement depuis le navigateur.

| Mode | URL |
|------|-----|
| Local / Docker Compose / Kubernetes | `http://localhost:3001` |

Fonctionnalités :
- Saisie du bucket et du fichier + chargement des colonnes (`GET /columns`)
- Sélection des colonnes de dates avec format DMY/MDY par colonne
- Lancement du traitement (`POST /processDate`) avec indicateur de progression et timer
- Tableau des 100 premières lignes avec colonnes de dates surlignées, durée et nombre total de lignes

---

## Endpoints

| Méthode | Route | Description |
|---------|-------|-------------|
| `GET` | `/columns?bucket=<bucket>&file=<nom>` | Liste les colonnes du fichier |
| `POST` | `/processDate` | Normalise les dates en place, retourne les 100 premières lignes |
| `GET` | `/health` | Health check |

### GET /columns

```bash
curl "http://localhost:3001/columns?bucket=arteci&file=lst_of_users_anon_1.csv"
```

### POST /processDate — body

```json
{
  "bucket": "arteci",
  "file": "lst_of_users_anon_1.csv",
  "date_columns": ["DATE_CREATION", "DATE_DESACTIVATION", "DATE_DERNIERE_CONNECTION_1"],
  "date_formats": ["MDY", "MDY", "MDY"]
}
```

**`date_formats`** : `MDY` (mois/jour/année — en_US) ou `DMY` (jour/mois/année — fr_FR). Résout les ambiguïtés pour les jours ≤ 12.

**Output** : `DD-MM-YYYY HH:mm:ss` (heure `00:00:00` si absente de la source).

**Écriture en place** : le fichier est modifié directement dans le bucket indiqué (`bucket`), au même chemin (`file`). Aucun bucket de destination séparé.

---

## Fichiers de test

Les fichiers CSV de test sont disponibles ici :
[Google Drive — Fixtures ARTECI](https://drive.google.com/drive/u/0/folders/1yhuNqSNO8FIw_vo5RRNe-UBNAmH9YnCN)

Placer les fichiers téléchargés dans le dossier `ressources/` à la racine du projet :

```
ARTECI/
└── ressources/
    ├── lst_of_users_anon_1.csv   (28 MB — 320K lignes)
    ├── lst_of_users_anon_2.csv   (182 MB — 2.1M lignes)
    └── lst_of_users_anon_3.csv   (931 MB — 10.8M lignes)
```

Au démarrage, l'API vérifie automatiquement si ces fichiers sont présents dans le bucket `arteci` et les uploade s'ils manquent.

---

## Configuration

```bash
cp .env.example .env
# Renseigner SIGNOZ_INGESTION_KEY (fourni séparément)
```

| Variable | Défaut | Description |
|----------|--------|-------------|
| `MINIO_ROOT_USER` | `minioadmin` | Identifiant MinIO |
| `MINIO_ROOT_PASSWORD` | `minioadmin` | Mot de passe MinIO |
| `MINIO_PORT` | `9000` | Port API MinIO |
| `MINIO_CONSOLE_PORT` | `9001` | Port console MinIO |
| `MINIO_BUCKET` | `arteci` | Nom du bucket |
| `MINIO_USE_SSL` | `false` | TLS MinIO |
| `API_PORT` | `3001` | Port de l'API Go |
| `OTEL_SERVICE_NAME` | `arteci-api-go` | Nom du service dans SigNoz |
| `SIGNOZ_INGESTION_KEY` | *(requis pour Cloud)* | Clé d'authentification SigNoz Cloud |

> Les valeurs par défaut s'appliquent si la variable est absente — le projet fonctionne sans `.env` (sans observabilité Cloud).

---

## Prérequis

### Observabilité — SigNoz

Le projet exporte traces, métriques et logs via OpenTelemetry vers **SigNoz**. Les options A et B nécessitent de configurer SigNoz avant le démarrage. Deux possibilités :

#### Option 1 — SigNoz Cloud

Aucune installation locale. Les données sont envoyées vers l'instance Cloud managée.

La clé d'ingestion et l'accès à la plateforme sont fournis séparément via **Doppler** (lien inbox joint à la soumission) — aucun compte à créer.

Une fois la clé reçue, la renseigner dans `.env` :

```bash
SIGNOZ_INGESTION_KEY=<clé-reçue-via-doppler>
```

L'API se connecte automatiquement à `ingest.us2.signoz.cloud:443` avec TLS. Les traces, métriques et logs sont visibles directement sur la plateforme partagée.

#### Option 2 — SigNoz self-hosted

SigNoz tourne localement via Docker Compose. Aucune clé requise.

```bash
docker compose -f docker/docker-compose.signoz.yml up -d
```

SigNoz UI disponible sur `http://localhost:8080` (premier démarrage : ~2 min le temps que ClickHouse s'initialise).

Laisser `SIGNOZ_INGESTION_KEY` vide dans `.env` — l'exporteur bascule automatiquement en mode non-TLS vers le collector local.

---

### Option A — Sans Docker

| Outil | Mac | Linux | Windows |
|-------|-----|-------|---------|
| Go 1.25+ | `brew install go` | [go.dev/dl](https://go.dev/dl/) | [go.dev/dl](https://go.dev/dl/) |
| Docker (pour MinIO) | [Docker Desktop](https://www.docker.com/products/docker-desktop/) | `apt install docker.io` | [Docker Desktop](https://www.docker.com/products/docker-desktop/) |

### Option B — Docker Compose

| Outil | Mac | Linux | Windows |
|-------|-----|-------|---------|
| Docker + Compose | [Docker Desktop 4.0+](https://www.docker.com/products/docker-desktop/) | `apt install docker.io docker-compose-plugin` | [Docker Desktop](https://www.docker.com/products/docker-desktop/) — WSL2 recommandé |

### Option C — Kubernetes

| Outil | Mac | Linux | Windows |
|-------|-----|-------|---------|
| Vagrant 2.3+ | `brew install vagrant` | [vagrantup.com](https://developer.hashicorp.com/vagrant/downloads) | [vagrantup.com](https://developer.hashicorp.com/vagrant/downloads) |
| QEMU | `brew install qemu` | `apt install qemu-system-arm` / `qemu-system-x86` | ⚠️ non supporté (voir note) |
| Plugin Vagrant QEMU | `vagrant plugin install vagrant-qemu` | idem | N/A |
| kubectl | `brew install kubectl` | [k8s.io](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/) | `winget install Kubernetes.kubectl` |
| helm 3+ | `brew install helm` | [helm.sh](https://helm.sh/docs/intro/install/) | `winget install Helm.Helm` |

> **Windows** : le provider QEMU n'est pas supporté nativement. Deux options :
> - Utiliser WSL2 et lancer les commandes Vagrant depuis le terminal WSL2.
> - Remplacer le provider QEMU par VirtualBox dans le `Vagrantfile` (supprimer le bloc `config.vm.provider "qemu"`, ajouter `config.vm.provider "virtualbox"`).

---

## Démarrage rapide

### Option A — Sans Docker

Prérequis : Go 1.25+, une instance MinIO accessible.

```bash
# 1. Créer le .env à la racine
cp .env.example .env
# Renseigner SIGNOZ_INGESTION_KEY si observabilité Cloud souhaitée

# 2. Démarrer MinIO — credentials lus depuis .env via --env-file
docker run -d -p 9000:9000 -p 9001:9001 \
  --env-file .env \
  minio/minio server /data \
  --address :9000 --console-address :9001

# 3. Lancer l'API depuis go/
cd go && go run .
```

> Si `MINIO_PORT` ou `MINIO_CONSOLE_PORT` ont été modifiés dans `.env`, ajuster les `-p` en conséquence.

L'API charge automatiquement `.env` à la racine, puis `go/.env` en fallback.

Au démarrage, le bucket `arteci` est créé automatiquement et les fichiers de `ressources/` sont uploadés s'ils sont absents.

### Option B — Avec Docker Compose

```bash
# 1. Créer le fichier de config
cp .env.example .env
# Renseigner SIGNOZ_INGESTION_KEY si mode Cloud (voir section SigNoz ci-dessus)

# 2. Démarrer la stack
docker compose --env-file .env -f docker/docker-compose.yml up -d --build
```

| Service | URL |
|---------|-----|
| API Go + Démo UI | `http://localhost:3001` |
| MinIO Console | `http://localhost:9001` (minioadmin / minioadmin) |
| SigNoz Cloud | `https://app.us2.signoz.cloud` |

Les traces, logs et métriques sont automatiquement envoyés vers SigNoz Cloud. Sélectionner le service `arteci-api-go` dans l'onglet **Services**.

Pour arrêter (volumes préservés — MinIO garde les fichiers, redémarrage quasi instantané) :

```bash
docker compose --env-file .env -f docker/docker-compose.yml down
```

Pour tout supprimer (volumes inclus — MinIO re-seedera au prochain démarrage) :

```bash
docker compose --env-file .env -f docker/docker-compose.yml down -v
```

#### Option B2 — SigNoz self-hosted

Si tu préfères faire tourner SigNoz localement :

```bash
# 1. Démarrer SigNoz (crée le réseau arteci)
docker compose -f docker/docker-compose.signoz.yml up -d

# 2. Modifier docker/docker-compose.yml :
#    OTEL_EXPORTER_OTLP_ENDPOINT: signoz-ingester:4317
#    (supprimer la ligne OTEL_EXPORTER_OTLP_HEADERS)
#    networks.arteci: external: true  name: arteci

# 3. Démarrer la stack arteci
docker compose -f docker/docker-compose.yml up -d --build
```

SigNoz UI disponible sur `http://localhost:8080`. Voir `docker/docker-compose.signoz.yml` pour l'architecture complète (7 services : ClickHouse Keeper, ClickHouse, PostgreSQL, migrator, ingester, app).

### Option C — Kubernetes (k3s via Vagrant)

Prérequis : Vagrant + plugin QEMU (`vagrant plugin install vagrant-qemu`) + `envsubst` (`brew install gettext` sur Mac, pré-installé sur Linux).

```bash
# 1. Créer le .env à la racine (si pas déjà fait)
cp .env.example .env
# Renseigner SIGNOZ_INGESTION_KEY si souhaité (optionnel en k8s — SigNoz est self-hosted)

# 2. Démarrer la VM k3s
cd Vagrant && vagrant up

# 3. Déployer toute la stack en une commande
./deploy-k8s.sh
```

Le script `deploy-k8s.sh` :
- Lit `.env` à la racine du projet (source de vérité unique)
- Crée le Secret k8s depuis les variables (`MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD`) — jamais committé
- Applique les manifests via `envsubst` pour injecter les valeurs (`MINIO_PORT`, `MINIO_BUCKET`, `API_PORT`…)
- Installe **SigNoz** via Helm (namespace `monitoring`)
- Attend que MinIO, le job d'init et l'API soient prêts

Accès à SigNoz après déploiement :

```bash
kubectl port-forward svc/signoz-signoz-0 8080:8080 -n monitoring
# Ouvrir http://localhost:8080
```

Pour arrêter la VM :

```bash
vagrant destroy -f
```

---

### Tester l'API

```bash
# Health check
curl http://localhost:3001/health

# Lister les colonnes d'un fichier
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

## Observabilité — OTel → SigNoz

L'API exporte traces, métriques et logs structurés via OTLP gRPC.

| Mode | Backend | Accès |
|------|---------|-------|
| Docker Compose (par défaut) | SigNoz Cloud | `https://app.us2.signoz.cloud` |
| Docker Compose (self-hosted) | SigNoz local | `http://localhost:8080` |
| Kubernetes | SigNoz self-hosted (Helm) | `kubectl port-forward svc/signoz-signoz-0 8080:8080 -n monitoring` |

### Vérifier avec SigNoz Cloud

1. Ouvrir `https://app.us2.signoz.cloud`
2. Aller dans **Services** → sélectionner `arteci-api-go`
3. Envoyer quelques requêtes à l'API, puis vérifier :
   - **Traces** : chaque appel `/processDate` ou `/columns` apparaît avec durée et spans
   - **Logs** : onglet **Logs** → filtrer `service.name = arteci-api-go`
   - **Metrics** : onglet **Metrics** → `http.server.request.duration`

> La clé `SIGNOZ_INGESTION_KEY` doit être renseignée dans `.env` (racine). Si absente, l'API démarre mais les données ne sont pas envoyées vers Cloud.

### Vérifier avec SigNoz self-hosted

**Docker Compose :**
```bash
# Démarrer SigNoz
docker compose -f docker/docker-compose.signoz.yml up -d
# Ouvrir http://localhost:8080
```

**Kubernetes :**
```bash
kubectl port-forward svc/signoz-signoz-0 8080:8080 -n monitoring
# Ouvrir http://localhost:8080
```

---


