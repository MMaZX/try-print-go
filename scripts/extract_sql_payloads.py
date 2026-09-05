#!/usr/bin/env python3
"""
Script de utilidad para extraer payloads JSON de sentencias SQL INSERT de la tabla
cola_impresion y generar automáticamente los archivos JSON formateados y sus vistas
previas en PNG.

Uso:
  python3 scripts/extract_sql_payloads.py <archivo.sql> [--out dist/rest] [--preview]

Ejemplos:
  # Solo extraer y formatear JSONs a dist/rest/
  python3 scripts/extract_sql_payloads.py mis_inserts.sql

  # Extraer y renderizar inmediatamente las vistas previas PNG
  python3 scripts/extract_sql_payloads.py mis_inserts.sql --preview

  # Especificar carpeta de salida personalizada
  python3 scripts/extract_sql_payloads.py mis_inserts.sql --out dist/comandas_demo --preview
"""

import argparse
import json
import os
import re
import subprocess
import sys


def parse_sql_inserts(sql_content):
    """Extrae las tuplas (id, payload_json_string) de sentencias INSERT."""
    # Busca patrones tipo: VALUES (id, ..., '{"options"...}', ...)
    pattern = re.compile(
        r"VALUES\s*\(\s*(\d+),\s*[^,]+,\s*[^,]+,\s*'({.+?})'\s*,\s*['\w]+",
        re.DOTALL | re.IGNORECASE,
    )
    matches = pattern.findall(sql_content)
    results = []

    for row_id_str, raw_payload in matches:
        row_id = int(row_id_str)
        # Desescapar comillas SQL: \' -> '
        unescaped = raw_payload.replace(r"\'", "'")
        try:
            parsed_json = json.loads(unescaped)
        except Exception:
            try:
                parsed_json = json.loads(raw_payload)
            except Exception as e:
                print(f"⚠️  [ID {row_id}] Error parseando JSON: {e}", file=sys.stderr)
                continue

        results.append((row_id, parsed_json))

    return results


def main():
    parser = argparse.ArgumentParser(
        description="Extrae payloads JSON de SQL y genera vistas previas PNG."
    )
    parser.add_argument("sql_file", help="Ruta al archivo .sql con los inserts")
    parser.add_argument(
        "--out",
        default="dist/rest",
        help="Directorio de salida relativo a la raíz (default: dist/rest)",
    )
    parser.add_argument(
        "--preview",
        action="store_true",
        default=True,
        help="Generar automáticamente las imágenes PNG con preview_ticket (default: True)",
    )
    parser.add_argument(
        "--no-preview",
        action="store_false",
        dest="preview",
        help="Solo extraer los archivos JSON sin renderizar PNGs",
    )

    args = parser.parse_args()

    if not os.path.exists(args.sql_file):
        print(f"❌ Archivo no encontrado: {args.sql_file}", file=sys.stderr)
        sys.exit(1)

    # Directorio base del proyecto
    script_dir = os.path.dirname(os.path.abspath(__file__))
    project_root = os.path.abspath(os.path.join(script_dir, ".."))
    out_dir_root = os.path.abspath(os.path.join(project_root, args.out))
    out_dir_client = os.path.abspath(os.path.join(project_root, "usqay-print-client", args.out))

    os.makedirs(out_dir_root, exist_ok=True)
    os.makedirs(out_dir_client, exist_ok=True)

    with open(args.sql_file, "r", encoding="utf-8", errors="replace") as f:
        content = f.read()

    rows = parse_sql_inserts(content)
    if not rows:
        print(f"⚠️  No se encontraron sentencias INSERT reconocibles en {args.sql_file}")
        sys.exit(0)

    print(f"==================================================")
    print(f"📦 Extrayendo {len(rows)} payloads SQL hacia {args.out}/...")
    print(f"==================================================")

    generated_files = []
    for row_id, data in rows:
        filename = f"cola_{row_id:02d}.json"
        formatted_json = json.dumps(data, indent=2, ensure_ascii=False) + "\n"

        # Guardar tanto en root como en cliente para conveniencia
        for target_dir in (out_dir_root, out_dir_client):
            file_path = os.path.join(target_dir, filename)
            with open(file_path, "w", encoding="utf-8") as f:
                f.write(formatted_json)

        generated_files.append((row_id, filename, os.path.join(out_dir_root, filename)))
        print(f"  📄 Generado: {filename}")

    if args.preview:
        print(f"\n==================================================")
        print(f"🎨 Renderizando vistas previas PNG con preview_ticket...")
        print(f"==================================================")

        client_dir = os.path.join(project_root, "usqay-print-client")
        for row_id, json_name, json_full_path in generated_files:
            png_name = f"cola_{row_id:02d}.png"
            png_root_path = os.path.join(out_dir_root, png_name)
            png_client_path = os.path.join(out_dir_client, png_name)

            cmd = [
                "go", "run", "./cmd/preview_ticket",
                json_full_path, png_client_path
            ]
            res = subprocess.run(cmd, cwd=client_dir, capture_output=True, text=True)
            if res.returncode != 0:
                print(f"  ❌ Error renderizando {json_name}: {res.stderr.strip()}")
            else:
                # Copiar también a root
                if os.path.exists(png_client_path):
                    with open(png_client_path, "rb") as rf, open(png_root_path, "wb") as wf:
                        wf.write(rf.read())
                print(f"  🖼️  Renderizado: {png_name}")

    print(f"==================================================")
    print(f"✅ Proceso completado exitosamente.")
    print(f"📁 Archivos listos en: {out_dir_root}/")
    print(f"==================================================")


if __name__ == "__main__":
    main()
