#!/usr/bin/env bash
# scripts/verify_pagination.sh
#
# QA check for anchor_get_registries pagination against a running MCP HTTP
# server. Exercises both paging styles the tool exposes on the unfiltered
# listing -- offset/limit and cursor (key = previous next_key) -- and the
# invariants a client relies on:
#
#   * pagination.total is the table size, not the page size, and is the
#     same on every page
#   * pages are disjoint, contiguous, and ordered by id
#   * walking by offset and walking by cursor produce identical pages
#   * next_key is present exactly while rows remain and absent on the last
#     page; offset + limit >= total agrees with it
#   * an offset past the end returns zero rows and the same total
#   * invalid combinations (key + offset, key + name, key + registry_id,
#     non-base64 key) are rejected before any chain call
#   * the name-filter path still pages its match set by offset/limit
#
# Everything here is read-only.
#
# Usage:
#   MCP_URL=http://localhost:8180 ./scripts/verify_pagination.sh
#   MCP_URL=... MCP_API_KEY=... PAGE=25 PAGES=6 ./scripts/verify_pagination.sh
#
# Optional:
#   PAGE    rows per page for the walks (default 10)
#   PAGES   how many pages to walk in each mode (default 5)
#   NAME    registry name for the name-filter check (default: the first
#           registry's name, looked up at runtime)

set -euo pipefail

for bin in jq curl; do
	if ! command -v "$bin" >/dev/null 2>&1; then
		echo "$bin is required" >&2
		exit 2
	fi
done

MCP_URL="${MCP_URL:-http://localhost:8180/}"
case "$MCP_URL" in
	*/) ;;
	*) MCP_URL="${MCP_URL}/" ;;
esac
PAGE="${PAGE:-10}"
PAGES="${PAGES:-5}"

pass=0
fail=0
skip=0
ok() { pass=$((pass + 1)); printf '  PASS  %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf '  FAIL  %s\n' "$1"; }
na() { skip=$((skip + 1)); printf '  SKIP  %s\n' "$1"; }

SESSION_ID=""
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

curl_mcp() {
	if [[ -n "${MCP_API_KEY:-}" ]]; then
		curl -sS "$@" -H "Authorization: Bearer ${MCP_API_KEY}"
	else
		curl -sS "$@"
	fi
}

unwrap() {
	local sse
	sse=$(printf '%s\n' "$1" | sed -n 's/^data:[[:space:]]*//p')
	if [[ -n "$sse" ]]; then printf '%s\n' "$sse"; else printf '%s\n' "$1"; fi
}

# tool_text prints the error text of a failed call, or nothing on success.
tool_text() {
	jq -r '
		if .error then "rpc: \(.error.message // .error)"
		elif .result.isError == true then
			([.result.content[]? | select(.type=="text") | .text] | join(" "))
		else empty
		end
	' <<<"$1"
}

tool_struct() { jq -c '.result.structuredContent // empty' <<<"$1"; }

handshake() {
	local hdrs body
	hdrs=$(mktemp "$workdir/hdrs.XXXXXX")
	body=$(curl_mcp -D "$hdrs" -X POST "$MCP_URL" \
		-H "Content-Type: application/json" \
		-H "Accept: application/json, text/event-stream" \
		-d '{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"verify-pagination","version":"1.0.0"}}}')
	SESSION_ID=$(grep -i '^mcp-session-id:' "$hdrs" | head -1 | awk '{print $2}' | tr -d '\r\n')
	if [[ -z "$SESSION_ID" ]]; then
		echo "ERROR: no Mcp-Session-Id from $MCP_URL" >&2
		unwrap "$body" >&2
		[[ -z "${MCP_API_KEY:-}" ]] && echo "HINT: set MCP_API_KEY if this server requires auth" >&2
		exit 2
	fi
	curl_mcp -X POST "$MCP_URL" \
		-H "Content-Type: application/json" \
		-H "Accept: application/json, text/event-stream" \
		-H "Mcp-Session-Id: $SESSION_ID" \
		-d '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' >/dev/null
}

# list ARGS_JSON [timeout] -> structuredContent of anchor_get_registries, or
# exits non-zero with the error text on stdout.
list() {
	local raw body err
	raw=$(curl_mcp --max-time "${2:-60}" -X POST "$MCP_URL" \
		-H "Content-Type: application/json" \
		-H "Accept: application/json, text/event-stream" \
		-H "Mcp-Session-Id: $SESSION_ID" \
		-d "$(jq -nc --argjson a "$1" '{jsonrpc:"2.0",method:"tools/call",id:2,params:{name:"anchor_get_registries",arguments:$a}}')")
	body=$(unwrap "$raw")
	err=$(tool_text "$body")
	if [[ -n "$err" ]]; then
		printf '%s\n' "$err"
		return 1
	fi
	tool_struct "$body"
}

ids_of() { jq -c '[.registries[].id]' <<<"$1"; }
total_of() { jq -r '.pagination.total // 0' <<<"$1"; }
next_of() { jq -r '.pagination.next_key // empty' <<<"$1"; }
rows_of() { jq -r '.registries | length' <<<"$1"; }
lb_of() { jq -r '.total_is_lower_bound // false' <<<"$1"; }

echo "MCP_URL=$MCP_URL PAGE=$PAGE PAGES=$PAGES"
handshake
echo "session $SESSION_ID"
echo

# ---------------------------------------------------------------------------
echo "== first page: total is the table size, not the page size =="
first=$(list "$(jq -nc --argjson l "$PAGE" '{limit:$l}')") || { bad "first page errored: $first"; echo "----"; echo "PASS=$pass FAIL=$fail SKIP=$skip"; exit 1; }
TOTAL=$(total_of "$first")
rows=$(rows_of "$first")
nk=$(next_of "$first")
if [[ "$TOTAL" -le 0 ]]; then
	bad "pagination.total=$TOTAL, want > 0"
elif [[ "$rows" -eq "$PAGE" && "$TOTAL" -eq "$PAGE" && -n "$nk" ]]; then
	bad "total=$TOTAL equals the page size while next_key is present (page size leaked into total)"
else
	ok "total=$TOTAL rows=$rows next_key=${nk:-<none>}"
fi
if [[ "$(lb_of "$first")" == "true" ]]; then
	bad "total_is_lower_bound=true on the first page (peek failed?)"
else
	ok "total_is_lower_bound absent (total is exact)"
fi
if [[ "$TOTAL" -gt "$PAGE" && -z "$nk" ]]; then
	bad "total=$TOTAL > limit=$PAGE but no next_key"
elif [[ "$TOTAL" -le "$PAGE" && -n "$nk" ]]; then
	bad "total=$TOTAL <= limit=$PAGE but next_key present"
else
	ok "next_key presence agrees with offset+limit<total"
fi
echo

if [[ "$TOTAL" -lt $((PAGE * 2)) ]]; then
	echo "table has only $TOTAL rows; walking fewer pages"
	PAGES=$(((TOTAL + PAGE - 1) / PAGE))
fi

# ---------------------------------------------------------------------------
echo "== offset walk: $PAGES pages of $PAGE =="
declare -a off_pages=()
off_ok=1
prev_last=0
for ((i = 0; i < PAGES; i++)); do
	off=$((i * PAGE))
	p=$(list "$(jq -nc --argjson o "$off" --argjson l "$PAGE" '{offset:$o,limit:$l}')") || { bad "offset=$off errored: $p"; off_ok=0; break; }
	ids=$(ids_of "$p")
	off_pages+=("$ids")
	t=$(total_of "$p")
	n=$(rows_of "$p")
	nk=$(next_of "$p")
	expect_n=$PAGE
	[[ $((off + PAGE)) -gt "$TOTAL" ]] && expect_n=$((TOTAL - off))
	[[ "$expect_n" -lt 0 ]] && expect_n=0
	if [[ "$t" -ne "$TOTAL" ]]; then bad "offset=$off total=$t drifted from $TOTAL"; off_ok=0; fi
	if [[ "$n" -ne "$expect_n" ]]; then bad "offset=$off rows=$n want $expect_n"; off_ok=0; fi
	# strictly increasing ids, and first id > last id of previous page
	first_id=$(jq -r '.[0] // 0' <<<"$ids")
	last_id=$(jq -r '.[-1] // 0' <<<"$ids")
	sorted=$(jq -r 'if . == (. | sort | unique) then "y" else "n" end' <<<"$ids")
	if [[ "$sorted" != "y" ]]; then bad "offset=$off ids not strictly increasing: $ids"; off_ok=0; fi
	if [[ "$n" -gt 0 && "$first_id" -le "$prev_last" ]]; then bad "offset=$off overlaps previous page (first=$first_id prev_last=$prev_last)"; off_ok=0; fi
	prev_last=$last_id
	more_expected=$(( (off + PAGE) < TOTAL ? 1 : 0 ))
	if [[ "$more_expected" -eq 1 && -z "$nk" ]]; then bad "offset=$off: more rows exist but no next_key"; off_ok=0; fi
	if [[ "$more_expected" -eq 0 && -n "$nk" ]]; then bad "offset=$off: last page but next_key present"; off_ok=0; fi
done
[[ "$off_ok" -eq 1 ]] && ok "$PAGES offset pages: disjoint, ordered, total constant, next_key consistent"
echo

# ---------------------------------------------------------------------------
echo "== cursor walk: $PAGES pages of $PAGE via key=next_key =="
declare -a cur_pages=()
cur_ok=1
key=""
for ((i = 0; i < PAGES; i++)); do
	if [[ -z "$key" ]]; then
		args=$(jq -nc --argjson l "$PAGE" '{limit:$l}')
	else
		args=$(jq -nc --arg k "$key" --argjson l "$PAGE" '{key:$k,limit:$l}')
	fi
	p=$(list "$args") || { bad "cursor page $i errored: $p"; cur_ok=0; break; }
	cur_pages+=("$(ids_of "$p")")
	t=$(total_of "$p")
	if [[ "$t" -ne "$TOTAL" ]]; then bad "cursor page $i total=$t drifted from $TOTAL"; cur_ok=0; fi
	key=$(next_of "$p")
	if [[ -z "$key" && $((i + 1)) -lt "$PAGES" ]]; then
		bad "cursor ended after page $i but $PAGES pages expected (total=$TOTAL)"
		cur_ok=0
		break
	fi
done
[[ "$cur_ok" -eq 1 ]] && ok "$PAGES cursor pages fetched, total constant"
echo

echo "== offset walk and cursor walk yield identical pages =="
if [[ "$off_ok" -eq 1 && "$cur_ok" -eq 1 ]]; then
	same=1
	for ((i = 0; i < ${#off_pages[@]}; i++)); do
		if [[ "${off_pages[$i]}" != "${cur_pages[$i]:-}" ]]; then
			bad "page $i differs: offset=${off_pages[$i]} cursor=${cur_pages[$i]:-<missing>}"
			same=0
		fi
	done
	[[ "$same" -eq 1 ]] && ok "all $PAGES pages identical (first page ids: ${off_pages[0]})"
else
	na "a walk failed above"
fi
echo

# ---------------------------------------------------------------------------
echo "== last page by offset ends the table =="
last_off=$(((TOTAL - 1) / PAGE * PAGE))
p=$(list "$(jq -nc --argjson o "$last_off" --argjson l "$PAGE" '{offset:$o,limit:$l}')") || { bad "offset=$last_off errored: $p"; p=""; }
if [[ -n "$p" ]]; then
	n=$(rows_of "$p")
	nk=$(next_of "$p")
	last_id=$(jq -r '.registries[-1].id // 0' <<<"$p")
	if [[ "$n" -ne $((TOTAL - last_off)) ]]; then
		bad "offset=$last_off rows=$n want $((TOTAL - last_off))"
	elif [[ -n "$nk" ]]; then
		bad "last page still carries next_key=$nk"
	else
		ok "offset=$last_off rows=$n last_id=$last_id no next_key (total=$TOTAL)"
	fi
fi
echo

echo "== offset past the end: zero rows, same total =="
p=$(list "$(jq -nc --argjson o "$((TOTAL + 1000))" --argjson l "$PAGE" '{offset:$o,limit:$l}')") || { bad "errored: $p"; p=""; }
if [[ -n "$p" ]]; then
	n=$(rows_of "$p"); t=$(total_of "$p"); nk=$(next_of "$p")
	if [[ "$n" -eq 0 && "$t" -eq "$TOTAL" && -z "$nk" ]]; then
		ok "rows=0 total=$t no next_key"
	else
		bad "rows=$n total=$t next_key=${nk:-<none>}"
	fi
fi
echo

echo "== limit above the chain page cap is served whole =="
big=$((200 + 50))
if [[ "$TOTAL" -le 200 ]]; then
	na "table has $TOTAL rows; cannot exercise a >200 window"
else
	p=$(list "$(jq -nc --argjson l "$big" '{limit:$l}')" 90) || { bad "limit=$big errored: $p"; p=""; }
	if [[ -n "$p" ]]; then
		n=$(rows_of "$p")
		expect=$big; [[ "$expect" -gt "$TOTAL" ]] && expect=$TOTAL
		ids=$(ids_of "$p")
		contig=$(jq -r 'if . == [range(.[0]; .[0] + length)] then "y" else "n" end' <<<"$ids")
		if [[ "$n" -eq "$expect" && "$contig" == "y" ]]; then
			ok "limit=$big returned $n contiguous rows in one call"
		else
			bad "limit=$big rows=$n (want $expect) contiguous=$contig"
		fi
	fi
fi
echo

# ---------------------------------------------------------------------------
echo "== invalid cursor combinations are rejected =="
some_key=$(next_of "$first")
if [[ -z "$some_key" ]]; then
	na "no next_key available (table fits one page)"
else
	for case in \
		"key + non-zero offset|$(jq -nc --arg k "$some_key" '{key:$k,offset:5}')" \
		"key + name|$(jq -nc --arg k "$some_key" '{key:$k,name:"anything"}')" \
		"key + registry_id|$(jq -nc --arg k "$some_key" '{key:$k,registry_id:1}')" \
		"key not base64|$(jq -nc '{key:"not!base64"}')"; do
		label="${case%%|*}"; args="${case#*|}"
		if out=$(list "$args" 15); then
			bad "$label was accepted: $(jq -c '.pagination' <<<"$out")"
		else
			ok "$label rejected: $out"
		fi
	done
	if out=$(list "$(jq -nc --arg k "$some_key" '{key:$k,offset:0,limit:1}')" 30); then
		ok "key + explicit offset=0 accepted (ids=$(ids_of "$out"))"
	else
		bad "key + offset=0 rejected: $out"
	fi
fi
echo

# ---------------------------------------------------------------------------
echo "== name filter still pages its match set by offset/limit =="
NAME="${NAME:-$(jq -r '.registries[0].name // empty' <<<"$first")}"
if [[ -z "$NAME" ]]; then
	na "no registry name to filter on"
else
	echo "   (name=$NAME; full-table scan, may take up to 90s)"
	byname=$(list "$(jq -nc --arg n "$NAME" '{name:$n,match:"exact",limit:1}')" 120) || { bad "name filter errored: $byname"; byname=""; }
	if [[ -n "$byname" ]]; then
		mt=$(total_of "$byname"); n=$(rows_of "$byname"); nk=$(next_of "$byname")
		if [[ "$mt" -ge 1 && "$n" -eq 1 && -z "$nk" ]]; then
			ok "matches=$mt page=1 row, no cursor on the name path"
		else
			bad "matches=$mt rows=$n next_key=${nk:-<none>}"
		fi
		if [[ "$mt" -gt 1 ]]; then
			p2=$(list "$(jq -nc --arg n "$NAME" '{name:$n,match:"exact",offset:1,limit:1}')" 120) || { bad "name offset=1 errored: $p2"; p2=""; }
			if [[ -n "$p2" ]]; then
				a=$(jq -r '.registries[0].id' <<<"$byname"); b=$(jq -r '.registries[0].id // 0' <<<"$p2")
				if [[ "$b" -gt "$a" ]]; then ok "name offset=1 advanced past id $a to $b"; else bad "name offset=1 did not advance (a=$a b=$b)"; fi
			fi
		fi
	fi
fi
echo

echo "----"
echo "PASS=$pass FAIL=$fail SKIP=$skip"
[[ "$fail" -gt 0 ]] && exit 1
exit 0
