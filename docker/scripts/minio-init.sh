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
    echo "[minio-init] $f déjà présent, ignoré"
  else
    echo "[minio-init] upload $f..."
    mc cp "/data/ressources/$f" "local/arteci/$f"
    echo "[minio-init] $f uploadé"
  fi
done

echo "[minio-init] initialisation terminée"
