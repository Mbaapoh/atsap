<!-- OpenSpec: TRD-HLD-07 -->
# Deployment, Packaging & Lifecycle

## 1. Container Topology & Network Segmentation

AtsaPBX is distributed as standard container images orchestrated via Docker Compose (for single-node and partner edge sites) or Ansible (for production cluster management).

```text
                                  PUBLIC INTERNET / VOICE VLAN
                                               │
                       ┌───────────────────────┴───────────────────────┐
                       │                                               │
                       ▼                                               ▼
         TCP 443 (API / HTTPS / WSS)                  TCP 5061 (SIP/TLS) + UDP 10000-20000 (RTP)
         ┌─────────────────────────┐                  ┌─────────────────────────────────────────┐
         │     API Gateway /       │                  │       Asterisk 22.x LTS Media Engine    │
         │   Traefik Reverse Proxy │                  │   - PJSIP Transport TLS & UDP           │
         └─────────────┬───────────┘                  │   - RTP Port Pool 10000-20000/UDP       │
                       │                              └────────────────────┬────────────────────┘
                       │ Internal HTTP/2                                   │
                       ▼                                                   │ ARI WS & REST (Internal)
         ┌───────────────────────────────────────────────────────────┐     │
         │                 AtsaPBX Core API Monolith                 │<────┘
         │  - Identity, PBX, Telephony, Compliance, Licensing Modules│
         │  - Transactional Outbox Background Worker                 │
         └─────────────┬─────────────────────────────┬───────────────┘
                       │ SQL (pgxpool)               │ NATS TCP 4222
                       ▼                             ▼
         ┌─────────────────────────┐   ┌─────────────────────────┐   ┌─────────────────────────┐
         │      PostgreSQL 16      │   │      NATS JetStream     │   │     MinIO / S3 Object   │
         │  - Relational DB + RLS  │   │  - Durable Event Store  │   │  - Encrypted Recordings │
         │  - Transactional Outbox │   │  - Stream Tenant Topics │   │  - Voicemail & Prompts  │
         └─────────────────────────┘   └─────────────────────────┘   └─────────────────────────┘
         [────────────────────────────── PRIVATE DATA NETWORK ─────────────────────────────────]
```

### 1.1 Network Security Zones

1. **Edge Public DMZ:**
   - `443/TCP`: Public ingress for ConnectRPC API, Web Admin UI, and WebRTC signalling (WSS).
   - `5061/TCP`: Secure SIP signalling over TLS from partner carrier trunks and remote desk phones.
   - `10000-20000/UDP`: RTP/SRTP voice media ingress and egress.
2. **Internal Core Network:**
   - Asterisk ARI (`8088/TCP`) and AMI (`5038/TCP`) bound exclusively to private container bridge. No public access.
3. **Internal Storage Network:**
   - PostgreSQL (`5432/TCP`), NATS (`4222/TCP`), and MinIO (`9000/TCP`) accessible only by the AtsaPBX Core container.

---

## 2. T-12 Zero-Downtime Upgrade & Rollback (PRD T-12, EPIC-09)

In a telephony environment, disconnecting running calls for a software upgrade is unacceptable. AtsaPBX implements an **Expand/Contract** database schema evolution and a canary rollout model that achieves zero call drops during upgrades and guarantees complete rollback within 30 minutes (AC-09.4).

```mermaid
sequenceDiagram
    autonumber
    participant Ops as Ansible Orchestrator
    participant DB as PostgreSQL 16
    participant CoreOld as AtsaPBX Core (v1.0)
    participant CoreNew as AtsaPBX Core (v1.1)
    participant Ast as Asterisk 22.x

    Ops->>DB: Phase 1 (Expand): golang-migrate up (additive changes only)
    Note over DB: Add nullable columns, new tables, views.<br/>Zero destructive changes. v1.0 code continues running safely.
    
    Ops->>CoreNew: Phase 2: Start Canary Instance (v1.1)
    Ops->>CoreNew: Synthetic Health Check (DB, NATS, ARI connection, test call)
    
    Ops->>CoreOld: Phase 3: Traffic Switch & Drain
    Note over CoreOld,Ast: Existing active calls stay connected on v1.0 / Asterisk bridges.<br/>New incoming call setups route to v1.1.
    
    Ops->>CoreOld: Wait for active calls to complete (Drain window, e.g. 15m)
    Ops->>CoreOld: Stop v1.0 container
    
    alt Upgrade Successful
        Note over Ops,DB: Phase 4 (Contract): Applied after 7-day stability window.<br/>Removes deprecated columns and temporary triggers.
    else Upgrade Failure Detected (Rollback Triggered)
        Ops->>CoreOld: Start v1.0 container (Rollback image)
        Ops->>CoreNew: Stop v1.1 container
        Ops->>DB: golang-migrate down (only if required; expand schema is backward-compatible)
        Note over Ops: Full rollback completes in <30 minutes with zero data loss (AC-09.4).
    end
```

### 2.1 Database Migration Rules

1. **Non-Destructive Operations Only:** Migrations applied before code rollout may only add tables, add nullable columns, or create indexes concurrently (`CREATE INDEX CONCURRENTLY`).
2. **No Synchronous Column Renames:** Renaming a column requires a multi-release sequence (Add new column -> Dual write -> Backfill -> Read new -> Drop old in Contract phase).
3. **Backward Compatibility Gate:** The v1.0 application must successfully pass all integration tests against the v1.1 "Expanded" database schema.

---

## 3. Configuration Management with Ansible

Ansible is the authoritative configuration management tool for partner host provisioning, OS tuning, container lifecycles, and backups (PRD §5.1, DECISIONS D-01):

```text
deploy/ansible/
├── group_vars/
│   ├── all.yml               # Base defaults, container versions, port assignments
│   └── production.yml        # Partner-specific domains, TLS certificates, capacity settings
├── roles/
│   ├── os_hardening/         # Kernel tuning (ulimits, sysctl UDP buffer sizing for RTP)
│   ├── firewall/             # UFW / iptables rules restricting ingress to documented ports
│   ├── container_runtime/    # Docker CE & Docker Compose plugin installation
│   ├── postgres/             # PostgreSQL 16 container, volume persistence, WAL archiving
│   ├── nats/                 # NATS JetStream container with encrypted storage
│   ├── asterisk/             # Asterisk 22.x LTS container, PJSIP and ARI baseline config
│   ├── atsapbx_core/         # AtsaPBX monolithic binary container & environment config
│   └── monitoring/           # Prometheus node-exporter and vector log forwarder
└── site.yml                  # Master playbook executing idempotent host deployment
```

### 3.1 Host OS Prerequisites & Pre-Flight Verification (AC-09.6)

Before executing deployment, Ansible runs a pre-flight validation module checking:
- Linux kernel 5.15+ (Debian 12 / Ubuntu 22.04+ / RHEL 9+).
- Minimum hardware: 4 vCPU, 8 GB RAM, 50 GB NVMe storage (handles launch capacity of 500 concurrent WebRTC/SIP channels).
- Network: UDP buffer limits (`net.core.rmem_max >= 16777216`), file descriptors (`ulimit -n >= 65536`).

---

## 4. Support Diagnostic Bundle (`support-bundle.sh`) (AC-09.5)

To enable tier-3 remote support for partner-hosted systems without compromising end-user privacy or tenant data sovereignty, AtsaPBX includes an automated diagnostic generation script:

```bash
/usr/local/bin/atsapbx-support-bundle --output /var/log/atsapbx-bundle-$(date +%s).tar.gz
```

### 4.1 Content & Redaction Invariants (AC-09.5, BR-15)

The diagnostic bundle generator strictly enforces:
1. **Zero Call Audio (AC-09.5):** Audio files, recordings, and voicemail `.wav`/`.mp3` files are explicitly excluded.
2. **Zero Plaintext Credentials (BR-15):** The script executes a streaming regex scrubber replacing all SIP passwords, database credentials, JWT secrets, and AI provider API keys with `[REDACTED]`.
3. **Included Artifacts:**
   - Host OS kernel version, CPU/memory utilization, and disk free space.
   - Sanitized container stdout/stderr logs from the previous 48 hours.
   - Asterisk channel and PJSIP endpoint statistics (`core show channels`, `pjsip show endpoints`).
   - PostgreSQL schema version migration status and connection pool metrics.
   - Prometheus metrics snapshot and Alertmanager incident history.

---

## 5. Verification & Lifecycle Acceptance

1. **Automated Clean Install Test:** Executes Ansible playbook against a clean Debian 12 virtual machine; verifies fresh install finishes with healthy test calls in under 60 minutes (AC-09.1).
2. **Zero-Call-Drop Upgrade Test:** Simulates 50 active WebRTC calls; applies v1.1 database expand migration and triggers container rollout; verifies 100% of active calls remain connected to completion (AC-09.3).
3. **Canary Rollback Benchmark:** Forces synthetic error on canary container; triggers automated rollback to prior release image; verifies complete system restoration in under 30 minutes (AC-09.4).
4. **Diagnostic Bundle Sanitization Test:** Inspects generated diagnostic archive; asserts zero occurrences of passwords, auth tokens, private keys, or audio payloads.


