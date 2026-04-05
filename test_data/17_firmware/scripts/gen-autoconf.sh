#!/bin/bash
set -e

INPUT="$1"
OUTPUT="$2"

if [ -z "$INPUT" ] || [ -z "$OUTPUT" ]; then
    echo "Usage: $0 <.config> <autoconf.h>" >&2
    exit 1
fi

mkdir -p "$(dirname "$OUTPUT")"

cat > "$OUTPUT" << 'HEADER'
#ifndef __AUTOCONF_H
#define __AUTOCONF_H
HEADER

while IFS= read -r line; do
    [ -z "$line" ] && continue

    if [[ "$line" == "# CONFIG_"*" is not set" ]]; then
        name="${line#\# CONFIG_}"
        name="${name%% *}"
        echo "#define CONFIG_$name 0" >> "$OUTPUT"
    elif [[ "$line" =~ ^CONFIG_[A-Za-z0-9_]+=y$ ]]; then
        name="${line%%=*}"
        echo "#define $name 1" >> "$OUTPUT"
    elif [[ "$line" =~ ^CONFIG_[A-Za-z0-9_]+= ]]; then
        name="${line%%=*}"
        val="${line#*=}"
        echo "#define $name $val" >> "$OUTPUT"
    fi
done < "$INPUT"

echo "#endif" >> "$OUTPUT"
