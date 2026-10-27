#!/usr/bin/env python3
"""backfill_op_typed.py — ADR-0004 stage 3a: one-shot typed-core twins for
operational expenses that predate the stage-1 hook.

For every workspace expense (source_type=operational_expense) without a
bank_transactions twin (external_transaction_id = OP-<sourceId>), posts the
canonical transaction through the financial API with the SAME idempotency
contract stage 1 uses. Idempotent: existing twins (any status) are skipped;
re-running is safe.

Usage (on the VPS):
  python3 backfill_op_typed.py 2            # dry-run: lists what would post
  python3 backfill_op_typed.py 2 --apply    # posts the missing twins

Exit: 0 ok · 1 some posts failed · 2 could-not-measure.
"""
import json
import subprocess
import sys
import urllib.request

API = "http://127.0.0.1:8181"
DB_CONTAINER = "textile-erp-db"
DB_USER = "erp_user"


def q(sql):
    pw = subprocess.run(["docker", "exec", DB_CONTAINER, "printenv", "DB_PASSWORD"],
                        capture_output=True, text=True).stdout.strip()
    r = subprocess.run(["docker", "exec", "-e", "PGPASSWORD=" + pw, DB_CONTAINER,
                        "psql", "-U", DB_USER, "-d", "textile_erp", "-At", "-F", "|", "-c", sql],
                       capture_output=True, text=True)
    if r.returncode != 0:
        sys.stderr.write(r.stderr)
        sys.exit(2)
    return [ln.split("|") for ln in r.stdout.strip().splitlines() if ln]


def jwt(secret):
    import base64, hashlib, hmac, time
    b64 = lambda d: base64.urlsafe_b64encode(d).rstrip(b"=").decode()
    h = b64(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
    p = b64(json.dumps({"company_id": 2, "user_id": 1, "role": "owner",
                        "exp": int(time.time()) + 900}).encode())
    sig = b64(hmac.new(secret.encode(), f"{h}.{p}".encode(), hashlib.sha256).digest())
    return f"{h}.{p}.{sig}"


def main():
    company = sys.argv[1] if len(sys.argv) > 1 else "2"
    apply = "--apply" in sys.argv

    secret = subprocess.run(
        ["docker", "exec", "textile-erp-financial-api", "printenv", "JWT_SECRET"],
        capture_output=True, text=True).stdout.strip()
    token = jwt(secret)

    accounts = {a[0]: a[1] for a in q(f"""
        SELECT a->>'id', a->>'name' FROM financial_workspace_states s,
               jsonb_array_elements(s.state->'accounts') a
        WHERE s.company_id={company}""")}
    existing = {r[0] for r in q(
        f"SELECT external_transaction_id FROM bank_transactions WHERE company_id={company} AND external_transaction_id LIKE 'OP-%'")}
    rows = q(f"""
        SELECT e->>'sourceId', COALESCE(e->>'amount','0'),
               COALESCE(e->>'date',''), COALESCE(e->>'accountId',''),
               COALESCE(e->>'group',''), COALESCE(e->>'description',''),
               COALESCE(e->>'enteredBy','')
        FROM financial_workspace_states s,
             jsonb_array_elements(s.state->'expenses') e
        WHERE s.company_id={company}
          AND e->>'source_type'='operational_expense'
          AND COALESCE(e->>'sourceId','') <> ''
        ORDER BY (e->>'sourceId')::numeric""")

    missing = [r for r in rows if "OP-" + r[0] not in existing]
    print(f"operational expenses={len(rows)} existing twins={len(existing)} missing={len(missing)}")
    failed = 0
    for sid, amount, date, account_id, group, desc, operator in missing:
        name = accounts.get(account_id, "")
        if not name or float(amount) <= 0:
            print(f"  SKIP OP-{sid}: account/date/amount incomplete ({name!r}, {amount})")
            continue
        body = json.dumps({
            "external_transaction_id": "OP-" + sid,
            "idempotency_key": "operational-exp-" + sid,
            "bank_account_id": 0, "bank_account_name": name,
            "direction": "OUT", "amount": int(float(amount)),
            "transaction_date": date, "transaction_time": "",
            "transaction_type": "OTHER_PAYMENT", "party_id": 0, "party_name": "",
            "counter_account_id": 0, "counter_account_name": "",
            "description": (group + " " + desc).strip()[:180],
            "source": "OPERATIONAL", "raw_source_reference": operator,
        }).encode()
        if not apply:
            print(f"  DRY OP-{sid}: {amount} -> {name} @ {date}")
            continue
        req = urllib.request.Request(
            API + "/api/v1/financial/transactions", data=body, method="POST",
            headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                out = json.load(resp)
            print(f"  POST OP-{sid}: {out.get('status', resp.status)}")
        except urllib.error.HTTPError as e:
            print(f"  FAIL OP-{sid}: {e.code} {e.read()[:120]}")
            failed += 1
        except Exception as e:
            print(f"  FAIL OP-{sid}: {e}")
            failed += 1
    print("done" + (f" with {failed} failures" if failed else ""))
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
