#!/bin/sh
set -e

MINIO_ENDPOINT="${MINIO_ENDPOINT:-arteci-minio}"
MINIO_PORT="${MINIO_PORT:-9000}"
MINIO_USER="${MINIO_ROOT_USER:-minioadmin}"
MINIO_PASS="${MINIO_ROOT_PASSWORD:-minioadmin}"
BUCKET="${MINIO_BUCKET:-arteci}"

if ! command -v wget > /dev/null 2>&1; then
    apk add --no-cache wget 2>/dev/null || true
fi

mc alias set local "http://${MINIO_ENDPOINT}:${MINIO_PORT}" "$MINIO_USER" "$MINIO_PASS"

mc ls local/ --json | while IFS= read -r line; do
  bucket=$(echo "$line" | cut -d'"' -f8 | tr -d '/')
  if [ -n "$bucket" ] && [ "$bucket" != "$BUCKET" ]; then
    echo "[minio-init] suppression bucket inutilisé : $bucket"
    mc rb --force "local/$bucket" || true
  fi
done

mc mb --ignore-existing "local/$BUCKET"
echo "[minio-init] bucket '$BUCKET' prêt"

# Single source of truth for Google Drive IDs — go/internal/storage/drive-ids.env,
# also embedded into the Go binary and mounted into the k8s minio-init job.
# Pure shell (no grep — not present in minio/mc's base image).
get_drive_id() {
  [ -f /data/drive-ids.env ] || return
  while IFS='=' read -r name id; do
    if [ "$name" = "$1" ]; then
      echo "$id"
      return
    fi
  done < /data/drive-ids.env
}

drive_download() {
  local file_id="$1"
  local dest="$2"
  wget -q -O "$dest" \
    "https://drive.usercontent.google.com/download?id=${file_id}&export=download&confirm=t"
}

for f in lst_of_users_anon_1.csv lst_of_users_anon_1.xlsx; do
  if mc stat "local/$BUCKET/$f" > /dev/null 2>&1; then
    echo "[minio-init] $f already present, skipped"
    continue
  fi

  uploaded=0

  drive_id=$(get_drive_id "$f")
  if [ -n "$drive_id" ] && command -v wget > /dev/null 2>&1; then
    echo "[minio-init] downloading $f from Google Drive..."
    if drive_download "$drive_id" "/tmp/$f"; then
      mc cp "/tmp/$f" "local/$BUCKET/$f"
      rm -f "/tmp/$f"
      echo "[minio-init] $f uploaded from Google Drive"
      uploaded=1
    else
      echo "[minio-init] Google Drive unavailable for $f, trying local..."
      rm -f "/tmp/$f"
    fi
  fi

  if [ "$uploaded" = "0" ]; then
    if [ -f "/data/ressources/$f" ]; then
      echo "[minio-init] uploading $f from local ressources..."
      mc cp "/data/ressources/$f" "local/$BUCKET/$f"
    elif [ -f "/data/fixtures/$f" ]; then
      echo "[minio-init] uploading $f from local fixtures..."
      mc cp "/data/fixtures/$f" "local/$BUCKET/$f"
    else
      echo "[minio-init] WARNING: $f not found in any source"
    fi
  fi
done

echo "[minio-init] initialisation terminée"
