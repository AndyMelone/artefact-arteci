#!/bin/sh
set -e

if ! command -v wget > /dev/null 2>&1; then
    apk add --no-cache wget 2>/dev/null || true
fi

mc alias set local http://arteci-minio:9000 minioadmin minioadmin

mc ls local/ --json | while IFS= read -r line; do
  bucket=$(echo "$line" | cut -d'"' -f8 | tr -d '/')
  if [ -n "$bucket" ] && [ "$bucket" != "arteci" ]; then
    echo "[minio-init] suppression bucket inutilisé : $bucket"
    mc rb --force "local/$bucket" || true
  fi
done

mc mb --ignore-existing local/arteci
echo "[minio-init] bucket 'arteci' prêt"

get_drive_id() {
  case "$1" in
    lst_of_users_anon_2.csv) echo "1547HnOZWAGCE5YoweHhUuSd_1AiueqaP" ;;
    # lst_of_users_anon_1.csv) echo "<DRIVE_FILE_ID>" ;;
    # lst_of_users_anon_3.csv) echo "<DRIVE_FILE_ID>" ;;
    *) echo "" ;;
  esac
}

drive_download() {
  local file_id="$1"
  local dest="$2"
  wget -q -O "$dest" \
    "https://drive.usercontent.google.com/download?id=${file_id}&export=download&confirm=t"
}

for f in lst_of_users_anon_1.csv lst_of_users_anon_2.csv lst_of_users_anon_3.csv; do
  if mc stat "local/arteci/$f" > /dev/null 2>&1; then
    echo "[minio-init] $f already present, skipped"
    continue
  fi

  uploaded=0

  drive_id=$(get_drive_id "$f")
  if [ -n "$drive_id" ] && command -v wget > /dev/null 2>&1; then
    echo "[minio-init] downloading $f from Google Drive..."
    if drive_download "$drive_id" "/tmp/$f"; then
      mc cp "/tmp/$f" "local/arteci/$f"
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
      mc cp "/data/ressources/$f" "local/arteci/$f"
    elif [ -f "/data/fixtures/$f" ]; then
      echo "[minio-init] uploading $f from local fixtures..."
      mc cp "/data/fixtures/$f" "local/arteci/$f"
    else
      echo "[minio-init] WARNING: $f not found in any source"
    fi
  fi
done

echo "[minio-init] initialisation terminée"
