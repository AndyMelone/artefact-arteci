# ARTECI — API de normalisation de dates CSV/Excel

API haute performance pour normaliser des colonnes de dates dans des fichiers CSV/Excel stockés dans MinIO.

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

## Prérequis

### Option A — Sans Docker

| Outil | Mac | Linux | Windows |
|-------|-----|-------|---------|
| Go 1.22+ | `brew install go` | [go.dev/dl](https://go.dev/dl/) | [go.dev/dl](https://go.dev/dl/) |
| Docker (pour MinIO) | [Docker Desktop](https://www.docker.com/products/docker-desktop/) | `apt install docker.io` | [Docker Desktop](https://www.docker.com/products/docker-desktop/) |

### Option B — Docker Compose

| Outil | Mac | Linux | Windows |
|-------|-----|-------|---------|
| Docker + Compose | [Docker Desktop 4.0+](https://www.docker.com/products/docker-desktop/) | `apt install docker.io docker-compose-plugin` | [Docker Desktop](https://www.docker.com/products/docker-desktop/) — WSL2 recommandé |

> Go n'est pas nécessaire — le build se fait entièrement dans Docker.

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

### Option A — Sans Docker (Go uniquement)

Prérequis : Go 1.22+, une instance MinIO accessible.

```bash
# 1. Démarrer MinIO en local
docker run -d -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin \
  -e MINIO_ROOT_PASSWORD=minioadmin \
  minio/minio server /data --console-address :9001

# 2. Lancer l'API Go depuis la racine du projet
cd go
go run .
```

L'API démarre sur `:3001`. Les variables sont configurables via `go/.env` (copier depuis `.env.example`).

Au démarrage, le bucket `arteci` est créé automatiquement et les fichiers de `ressources/` sont uploadés s'ils sont absents.

### Option B — Avec Docker Compose

```bash
# 1. Démarrer SigNoz 
docker compose -f docker/docker-compose.signoz.yml up -d

# 2. Démarrer la stack arteci
docker compose -f docker/docker-compose.yml up -d --build
```

| Service | URL |
|---------|-----|
| API Go | `http://localhost:3001` |
| MinIO Console | `http://localhost:9001` (minioadmin / minioadmin) |
| SigNoz UI | `http://localhost:3301` |

Les traces OTel sont visibles dans SigNoz — sélectionner le service `arteci-api-go` dans l'onglet **Services**.

Pour arrêter et tout supprimer (volumes inclus) :

```bash
docker compose -f docker/docker-compose.yml down -v
docker compose -f docker/docker-compose.signoz.yml down -v
```

### Option C — Kubernetes (k3s via Vagrant)

Prérequis : Vagrant + plugin QEMU (`vagrant plugin install vagrant-qemu`).

```bash
# 1. Démarrer la VM k3s
cd Vagrant
vagrant up

# 2. Déployer toute la stack en une commande
./deploy-k8s.sh
```

Le script `deploy-k8s.sh` :
- Copie automatiquement le kubeconfig depuis la VM
- Installe **SigNoz** via Helm (namespace `monitoring`)
- Applique les manifests arteci dans le bon ordre
- Attend que MinIO, le job d'init et l'API soient prêts

Accès à SigNoz après déploiement :

```bash
kubectl port-forward svc/signoz-frontend 3301:3301 -n monitoring
# Ouvrir http://localhost:3301
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

L'API exporte traces, métriques et logs structurés via OTLP gRPC vers un OTel Collector.

| Mode | Backend traces | URL |
|------|---------------|-----|
| Docker Compose | SigNoz | `http://localhost:3301` |
| Kubernetes | SigNoz | `kubectl port-forward svc/signoz-frontend 3301:3301 -n monitoring` |
---


