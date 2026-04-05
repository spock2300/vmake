#!/bin/bash
set -e

KCONFIG="$1"
DOTCONFIG="$2"

if [ -z "$KCONFIG" ] || [ -z "$DOTCONFIG" ]; then
    echo "Usage: $0 <Kconfig> <.config>" >&2
    exit 1
fi

if ! command -v dialog &>/dev/null; then
    echo "Error: 'dialog' is required. Install: sudo apt install dialog" >&2
    exit 1
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

awk '
BEGIN { menu=""; name=""; type=""; desc=""; defval=""; depends="" }
/^menu[[:space:]]+"[^"]*"/ {
    m = substr($0, index($0, "\"")+1)
    gsub(/".*/, "", m)
    menu = m
}
/^config[[:space:]]+/ {
    if (name != "") printf "%s|%s|%s|%s|%s|%s\n", type, name, defval, desc, menu, depends
    name = $2; type=""; desc=""; defval=""; depends=""
}
/^[[:space:]]+(bool|string|int|hex)[[:space:]]/ {
    type = $1
    rest = substr($0, length($1)+2)
    gsub(/^[[:space:]]+/, "", rest)
    if (rest ~ /^".*"/) {
        desc = substr(rest, 2, length(rest)-2)
    }
}
/^[[:space:]]+default[[:space:]]+/ {
    defval = substr($0, index($0, "default")+8)
    gsub(/^[[:space:]]+/, "", defval)
    gsub(/"/, "", defval)
}
/^[[:space:]]+depends[[:space:]]+on[[:space:]]+/ {
    depends = substr($0, index($0, "on")+3)
    gsub(/^[[:space:]]+/, "", depends)
}
END {
    if (name != "") printf "%s|%s|%s|%s|%s|%s\n", type, name, defval, desc, menu, depends
}
' "$KCONFIG" > "$TMP/items"

declare -A CUR
if [ -f "$DOTCONFIG" ]; then
    while IFS= read -r line; do
        if [[ "$line" =~ ^CONFIG_([A-Za-z0-9_]+)=(.*) ]]; then
            CUR["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
        elif [[ "$line" == "# CONFIG_"*" is not set" ]]; then
            n="${line#\# CONFIG_}"; n="${n%% *}"
            CUR["$n"]="n"
        fi
    done < "$DOTCONFIG"
fi

get_val() { echo "${CUR[$1]:-$2}"; }
check_dep() {
    local dep_name="${1#CONFIG_}"
    [ "${CUR[$dep_name]:-n}" = "y" ]
}

ARGS=()
while IFS='|' read -r type name defval desc menu depends; do
    [ "$type" != "bool" ] && continue
    [ -n "$depends" ] && ! check_dep "$depends" && continue
    v="$(get_val "$name" "$defval")"
    s="off"; [ "$v" = "y" ] && s="on"
    ARGS+=("${menu:+[$menu] }$name" "$desc" "$s")
done < "$TMP/items"

if [ ${#ARGS[@]} -eq 0 ]; then
    echo "No configurable items." >&2; exit 0
fi

exec 3>&1
SEL=$(dialog --separate-output --checklist "Bool Options (Space=toggle, Enter=OK)" 20 76 12 \
    "${ARGS[@]}" 2>&1 1>&3 3>&-)
ret=$?
exec 3>&-
[ $ret -ne 0 ] && { echo "Cancelled." >&2; exit 0; }

> "$TMP/nonbool"
while IFS='|' read -r type name defval desc menu depends; do
    [ "$type" = "bool" ] && continue
    [ -n "$depends" ] && ! check_dep "$depends" && continue
    cur="$(get_val "$name" "$defval")"
    exec 3>&1
    new=$(dialog --inputbox "${menu:+[$menu] }$desc" 8 60 "$cur" 2>&1 1>&3 3>&-)
    r=$?
    exec 3>&-
    [ $r -ne 0 ] && { echo "Cancelled." >&2; exit 0; }
    echo "$type|$name|$new" >> "$TMP/nonbool"
done < "$TMP/items"

> "$TMP/newconfig"
while IFS='|' read -r type name defval desc menu depends; do
    [ "$type" != "bool" ] && continue
    found=0
    for n in $SEL; do [ "$n" = "$name" ] && found=1 && break; done
    if [ "$found" = "1" ]; then
        echo "CONFIG_$name=y"
    else
        echo "# CONFIG_$name is not set"
    fi
done < "$TMP/items" >> "$TMP/newconfig"

while IFS='|' read -r type name val; do
    if [ "$type" = "string" ]; then
        echo "CONFIG_$name=\"$val\""
    else
        echo "CONFIG_$name=$val"
    fi
done < "$TMP/nonbool" >> "$TMP/newconfig"

cp "$TMP/newconfig" "$DOTCONFIG"
echo "Configuration saved to $DOTCONFIG"
