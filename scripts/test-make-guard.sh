#!/bin/sh
# Table test for the Makefile's local-database guard (`make require-local-db`).
#
# The guard decides whether destructive targets (migrate-down, db-reset, test,
# ci) may run: only when the URL provably points at a local database. It parses
# URLs with sed, so it is tested against URLs that confuse parsers: a second
# "@" in the authority, query parameters that override the host, odd host
# spellings. Every URL carries SECRET as its password; no output may contain it.
#
# Run: sh scripts/test-make-guard.sh   (also: moon run api:guard)
# Needs only make; it never connects to a database.

cd "$(dirname "$0")/.." || exit 1

SECRET=SUPERSECRET
fail=0

check() { # check <allow|refuse> <url> [FORCE value]
	want=$1
	url=$2
	out=$(FORCE="$3" VELNEST_DB_URL="$url" make -s require-local-db 2>&1)
	rc=$?
	if [ "$want" = allow ]; then
		[ "$rc" -eq 0 ] && verdict=PASS || verdict=FAIL
	else
		[ "$rc" -ne 0 ] && verdict=PASS || verdict=FAIL
	fi
	case "$out" in *"$SECRET"*) verdict=FAIL; out="(output leaked the password)" ;; esac
	[ "$verdict" = PASS ] || fail=1
	printf '%-4s want=%-6s %s\n' "$verdict" "$want" "$url"
	[ "$verdict" = PASS ] || printf '       output: %s\n' "$out"
}

# --- provably local: allowed ---
check allow "postgres://velnest:$SECRET@localhost:54322/velnest?sslmode=disable"
check allow "postgres://velnest:$SECRET@localhost:54322/velnest"
check allow "postgres://velnest:$SECRET@127.0.0.1:54322/velnest?sslmode=disable&connect_timeout=5"
check allow "postgres://u:$SECRET@[::1]:5432/x"
check allow "postgres://u:$SECRET@localhost/x?sslmode=disable&application_name=a@evil.example"

# --- not local: refused ---
check refuse "postgres://u:$SECRET@db.example.com:5432/x"
check refuse "postgres://u:$SECRET@localhost.evil.example/x"
check refuse "postgres://u:$SECRET@LOCALHOST/x"
check refuse "postgres://u:$SECRET@localhost./x"
check refuse "postgres://u:$SECRET@localhost,prod.example/x"
check refuse "postgres:///velnest"
check refuse "host=prod.example user=x password=$SECRET dbname=y"

# --- query parameters that can override the host ---
check refuse "postgres://u:$SECRET@localhost:5432/x?host=prod.example"
check refuse "postgres://u:$SECRET@localhost:5432/x?sslmode=disable&host=prod.example"
check refuse "postgres://u:$SECRET@localhost:5432/x?hostaddr=10.1.2.3"
check refuse "postgres://u:$SECRET@localhost:5432/x?service=prod"
check refuse "postgres://u:$SECRET@localhost:5432/x?%68ost=prod.example"
check refuse "postgres://u:$SECRET@localhost:5432/x?HOST=prod.example"

# --- a second "@": URL parsers split at the LAST one, so the real host differs ---
check refuse "postgres://u:$SECRET@localhost:5432@evil.example/x"
check refuse "postgres://u:$SECRET@localhost@evil.example/x"
check refuse "postgres://u:$SECRET@evil.example@localhost/x"
check refuse "postgres://localhost?x=a@evil.example"

# --- FORCE=1 lifts the guard ---
check allow "postgres://u:$SECRET@db.example.com:5432/x" 1

if [ "$fail" -ne 0 ]; then
	echo "guard test FAILED" >&2
	exit 1
fi
echo "guard test passed"
