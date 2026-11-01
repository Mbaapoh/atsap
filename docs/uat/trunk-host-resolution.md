# UAT — Trunk host resolution (human pass in the browser)

User Acceptance Testing for OpenSpec change `pbx-trunk-host-resolution`
(D-75). The automated tiers prove the mechanism. In particular, the engine
makes zero name lookups while it identifies carrier calls, and a real
carrier reached by DNS name is recognised. This pass is what a partner
engineer sees in the console. The document is both the reusable procedure
and the executed record.

**Traceability:**
- BRD FBR-R1-03 (connect any carrier; a failing one is detected) and FBR-R1-04;
- PRD INV-10 (one tenant cannot affect another) and US-10.10 (see at once whether a trunk works);
- spec `openspec/specs/pbx-core/trunk-routing` ("Inbound signalling is accepted only from a trunk's carrier address");
- `openspec/changes/archive/2026-11-01-pbx-trunk-host-resolution`.

## 1. Environment

| Item | Detail |
|---|---|
| Stack | The dev rig (`make tilt-up`, or the compose stack with the app rebuilt) and the console dev server on **http://localhost:5173** |
| Login | `dev@example.test` / `e2e-fixture-operator-password` (`make dev-user`), tenant **Dev Acme** |
| Licence | The rig is licensed (`make dev-licence` once) |
| Mail | Password and invitation mail lands in Mailpit, http://localhost:8025 |

If the console is not running, start it with `make tilt-up` (preferred). Without Tilt, run `cd console && npm run dev`; it proxies the API on `localhost:8080`.

## 2. Procedure

| # | Step | Expected |
|---|---|---|
| 1 | Open http://localhost:5173, enter the email, press **Continue**, enter the password, press **Sign in**. If asked, choose **Dev Acme** | The Overview page of Dev Acme |
| 2 | Open **Trunks & routes** from the navigation (or http://localhost:5173/trunks) | The **Trunks** table, with columns *Host*, *Observed health* |
| 3 | Press **Add trunk**. Name `Typo carrier`, Host `locslhodt`, Channel limit `2`. Leave the rest. Press **Add trunk** | The trunk is **accepted** (owner decision: accept and flag), and reads **Not checked yet** at first |
| 4 | Wait about 10 seconds and reload | `Typo carrier` reads **Unhealthy · Host not found · checked <time>** |
| 5 | Press **Add trunk**. Name `Engine by name`, Host `asterisk` (the rig's engine, a name that resolves), Channel limit `2`. Press **Add trunk** | After a few seconds and a reload, it reads **Healthy · Answering · checked <time>** |
| 6 | Open **Actions for Typo carrier → Edit…**, change Host to `asterisk`, and press **Save changes** | It returns to **Not checked yet**, then turns **Healthy · Answering** within seconds; the old observation was of another address |
| 7 | (Optional, other tenants unaffected) Make a call between two registered phones or through `make uat-inbound` while a `locslhodt` trunk exists | Calls connect at normal speed. Before D-75, every carrier call waited on that name |
| 8 | Delete both test trunks: **Actions for … → Delete trunk… → Delete trunk** | Both are gone from the table |

What the console does **not** show, by design: the addresses a name resolved to. Those are engine-facing (PRD rule 4: no engine vocabulary on screen). To see them on the rig:

```bash
docker exec deploy-postgres-1 psql -U asterisk -d atsapbx -tA -c \
  "select c.name, c.host, c.resolved_addresses, coalesce(i.match,'(no row)')
   from carrier_trunks c left join ps_endpoint_id_ips i
   on i.id = 't_' || replace(c.id::text, '-', '') order by c.name"
```

The expected values:
- **A resolving name** (`asterisk`): its address in both columns, for example `{172.18.0.5}` and `172.18.0.5`.
- **The typo:** `{}` and `(no row)`. Never the name.

## 3. Executed record

| Date | 2026-11-01 |
|---|---|
| Who | Claude (scripted walk in Chromium of steps 1–5 and 8, against the rig app built from `develop`) |
| Result | **PASS** |

- `Typo carrier` (`locslhodt`) read **Unhealthy · Host not found · checked 10:37:11 AM**, with no identify row and no addresses.
- `Engine by name` (`asterisk`) read **Healthy · Answering · checked 10:37:13 AM**, with an identify row and addresses of `172.18.0.5`.
- The owner's existing `test` trunk (`localhost`) read **Unhealthy · No response**, recognised by `127.0.0.1,::1`.
- Both test trunks were deleted through the public API afterwards, and `pbx reconcile` reported nothing for them.
- A human pass of steps 1–8 by the owner is still to be recorded here.
