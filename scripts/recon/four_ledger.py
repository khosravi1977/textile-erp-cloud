#!/usr/bin/env python3
"""four_ledger.py — MASTER-RESCUE-10 F0 reconciliation tool.

Read-only. Compares the independently-stored views of the same financial
truth for one company and reports drift:

  VIEW A "displayed"  workspace account.opening + movement net (UI formula,
                      including transfer counter-legs) — what the dashboard
                      renders.
  VIEW B "reported"   workspace account.lastReportedBalance — the HesabYar
                      phone-reported truth anchor.
  VIEW C "typed"      bank_transactions (typed core) signed net per bank
                      account — the canonical posting store.

Invariants:
  I1 displayed == reported for every mobile-managed account (anchor works).
  I2 typed net + implied opening == reported (dual-write consistency).

Exit codes: 0 no drift · 1 drift · 2 could-not-measure.
Usage: four_ledger.py <company_id>            # via psql in docker on the VPS
       PGPASSWORD=... four_ledger.py <id> --dsn "host=... user=... dbname=..."
"""
import json
import subprocess
import sys

CONTAINER_DB = "textile-erp-db"
CONTAINER_USER = "erp_user"


def q(sql, dsn=None):
    if dsn:
        r = subprocess.run(["psql", dsn, "-At", "-F", "|", "-c", sql],
                           capture_output=True, text=True)
    else:
        pw = subprocess.run(["docker", "exec", CONTAINER_DB, "printenv", "DB_PASSWORD"],
                            capture_output=True, text=True).stdout.strip()
        r = subprocess.run(
            ["docker", "exec", "-e", "PGPASSWORD=" + pw, CONTAINER_DB, "psql",
             "-U", CONTAINER_USER, "-d", "textile_erp", "-At", "-F", "|", "-c", sql],
            capture_output=True, text=True)
    if r.returncode != 0:
        sys.stderr.write(r.stderr)
        sys.exit(2)
    return [ln.split("|") for ln in r.stdout.strip().splitlines() if ln]


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(2)
    company = sys.argv[1]
    dsn = None
    if "--dsn" in sys.argv:
        dsn = sys.argv[sys.argv.index("--dsn") + 1]

    # VIEW A + B: workspace accounts and movement nets (UI formula).
    ws = {}
    for aid, name, opening, lastrep, managed in q(f"""
        SELECT a->>'id', a->>'name',
               COALESCE(a->>'opening','0')::bigint,
               COALESCE(a->>'lastReportedBalance','') ,
               COALESCE(a->>'mobileManaged','false')
        FROM financial_workspace_states s,
             jsonb_array_elements(s.state->'accounts') a
        WHERE s.company_id={company}"""):
        ws[aid] = {"name": name, "opening": int(opening),
                   "reported": int(lastrep) if lastrep else None,
                   "managed": managed == "true", "net": 0}
    for aid, amt, d, typ, cta in q(f"""
        SELECT m->>'accountId', COALESCE(m->>'amount','0')::bigint,
               m->>'direction', COALESCE(m->>'transactionType',''),
               COALESCE(m->>'counterAccountId','')
        FROM financial_workspace_states s,
             jsonb_array_elements(s.state->'movements') m
        WHERE s.company_id={company}"""):
        if aid not in ws:
            continue
        amt = int(amt)
        if d == "in":
            ws[aid]["net"] += amt
        else:
            ws[aid]["net"] -= amt
        if typ == "transfer" and cta in ws:
            ws[cta]["net"] += amt if d == "out" else -amt

    # VIEW C: typed core net per bank account.
    typed = {}
    for name, net in q(f"""
        SELECT COALESCE(b.name,''), COALESCE(SUM(CASE WHEN t.direction='IN'
                    THEN t.amount ELSE -t.amount END),0)
        FROM bank_transactions t
        JOIN bank_accounts b ON b.id=t.bank_account_id AND b.company_id=t.company_id
        WHERE t.company_id={company} AND t.status='ACTIVE'
        GROUP BY b.name"""):
        typed[name] = int(net)

    drift = []
    print(f"{'account':<28} {'displayed':>16} {'reported':>16} {'typed':>16}  flags")
    for aid, a in sorted(ws.items(), key=lambda kv: kv[1]["name"]):
        displayed = a["opening"] + a["net"]
        reported = a["reported"]
        tnet = typed.get(a["name"])
        flags = []
        if a["managed"] and reported is not None and displayed != reported:
            flags.append("I1-displayed!=reported")
        if tnet is not None and reported is not None:
            implied = reported - tnet
            if implied != a["opening"]:
                flags.append(f"I2-implied-opening={implied}")
        print(f"{a['name']:<28} {displayed:>16} {str(reported or 0):>16} {str(tnet or 0):>16}  {' '.join(flags)}")
        if flags:
            drift.append({"account": a["name"], "flags": flags})
    print(f"\naccounts={len(ws)} typed_accounts={len(typed)} drift_rows={len(drift)}")
    if drift:
        print(json.dumps(drift, ensure_ascii=False, indent=1))
        sys.exit(1)
    sys.exit(0)


if __name__ == "__main__":
    main()
