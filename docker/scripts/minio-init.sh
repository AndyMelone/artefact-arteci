#!/bin/sh
set -e

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

for f in lst_of_users_anon_1.csv lst_of_users_anon_2.csv lst_of_users_anon_3.csv; do
  if mc stat "local/arteci/$f" > /dev/null 2>&1; then
    echo "[minio-init] $f already present, skipped"
  elif [ -f "/data/ressources/$f" ]; then
    echo "[minio-init] uploading $f from ressources..."
    mc cp "/data/ressources/$f" "local/arteci/$f"
  elif [ -f "/data/fixtures/$f" ]; then
    echo "[minio-init] uploading $f from fixtures (fallback)..."
    mc cp "/data/fixtures/$f" "local/arteci/$f"
  else
    echo "[minio-init] WARNING: $f not found in ressources or fixtures, skipped"
  fi
done

echo "[minio-init] initialisation terminée"
