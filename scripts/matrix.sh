#!/usr/bin/env bash
# Runs the contract suite for every target × engine × profile combination and writes the results table into README.md
# (between the matrix markers). Raw go test output is kept under var/matrix/<run-id>/.
# Usage: scripts/matrix.sh            Env: TARGETS="php go", ENGINES="sqlite postgres mysql maria mssql"
set -uo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
targets=${TARGETS:-php go}
engines=${ENGINES:-sqlite postgres mysql maria mssql}
profiles=${PROFILES:-default custom integrations}
run_id=$(date +%Y%m%d-%H%M%S)
out="var/matrix/$run_id"
mkdir -p "$out"

for profile in $profiles; do
  for db in $engines; do
    for target in $targets; do
      log="$out/$profile.$db.$target.log"
      echo "== $profile $db $target"
      PROFILE=$profile scripts/contract.sh "$target" "$db" >"$log" 2>&1
      grep -E "^(ok|FAIL|---)" "$log" | grep -v "no test files" | sed 's/^/   /'
    done
  done
done

python3 - "$out" "$profiles" "$engines" <<'PY'
import os, re, sys
out, profiles, engines = sys.argv[1], sys.argv[2].split(), sys.argv[3].split()
names = {"sqlite": "SQLite", "postgres": "PostgreSQL", "mysql": "MySQL", "maria": "MariaDB", "mssql": "MS SQL"}
suites = ["rest", "cli", "diff", "integration", "schema"]
def result(profile, db, target, suite):
    path = os.path.join(out, f"{profile}.{db}.{target}.log")
    if not os.path.exists(path):
        return None
    text = open(path).read()
    m = re.search(rf"^(ok|FAIL)\s+\S+/contract/{suite}\s+(.*)$", text, re.M)
    if not m or "[no tests to run]" in m.group(2):
        return None
    return m.group(1) == "ok"
rows = []
for profile in profiles:
    for suite in suites:
        cells, any_run = [], False
        for db in engines:
            r = [result(profile, db, t, suite) for t in ("php", "go")]
            if all(x is None for x in r):
                cells.append("–")
                continue
            any_run = True
            cells.append(" / ".join("–" if x is None else ("pass" if x else "**fail**") for x in r))
        if any_run:
            rows.append(f"| {suite} | {profile} | " + " | ".join(cells) + " |")
table = (f"Run `{os.path.basename(out)}`. Each cell is upstream / port; – means not run or no tests for that combination.\n\n"
         "| Suite | Profile | " + " | ".join(names[e] for e in engines) + " |\n|---|---|" + "---|" * len(engines) + "\n"
         + "\n".join(rows) + "\n")
print(table)
readme = open("README.md").read()
start, end = "<!-- matrix:start -->", "<!-- matrix:end -->"
i, j = readme.index(start), readme.index(end)
open("README.md", "w").write(readme[:i + len(start)] + "\n" + table + readme[j:])
PY
