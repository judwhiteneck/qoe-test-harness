# Deploy guide

Two pieces run in a real test:

- **`qoe-server`** — on your off-net VPS (Hostinger). Marks/echoes packets, gates
  the cookie handshake. **Linux only.**
- **`qoe-cli`** — on the tester machine, **wired to the modem**, pointed at the
  server. Marks packets, runs the phases, prints the verdict.

Only the server holds the shared secret; the CLI never needs it (it just echoes
the server's cookie).

> Status caveat: pass/fail thresholds are calibrated in M0 on a real LLD line, so
> until then `-run` will usually report **inconclusive** even on a good path. What
> *is* trustworthy today: marking survival, achieved throughput (capacity gate),
> and the LL-vs-classic latency distributions.

---

## 1. Server → VPS (Linux)

You can copy a prebuilt static binary (no Go needed on the VPS) or build from
source.

### Option A — copy the prebuilt binary

From your machine (the binary is `linux/amd64`, static — matches a Hostinger KVM2):

```bash
scp qoe-server root@YOUR_VPS_IP:/usr/local/bin/qoe-server
ssh root@YOUR_VPS_IP 'chmod +x /usr/local/bin/qoe-server'
```

### Option B — build from source on the VPS

```bash
ssh root@YOUR_VPS_IP
# install Go if absent:
curl -sL https://go.dev/dl/go1.23.4.linux-amd64.tar.gz | tar -C /usr/local -xz
export PATH=$PATH:/usr/local/go/bin
git clone https://github.com/judwhiteneck/qoe-test-harness && cd qoe-test-harness
go build -o /usr/local/bin/qoe-server ./server/cmd/qoe-server
```

### Open the UDP port and run it

Pick a port (e.g. 7700) and a strong shared secret.

```bash
# firewall (ufw shown; use your provider's panel too):
ufw allow 7700/udp

# run as a service so it survives reboots/SSH logout:
cat >/etc/systemd/system/qoe-server.service <<'UNIT'
[Unit]
Description=QoE LLD/L4S test server
After=network-online.target

[Service]
ExecStart=/usr/local/bin/qoe-server -addr :7700 -secret CHANGE_ME_LONG_RANDOM
Restart=always
User=nobody

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload && systemctl enable --now qoe-server
systemctl status qoe-server --no-pager
```

Sanity from your machine: `nc -u YOUR_VPS_IP 7700` won't show much (UDP), but the
CLI's handshake will confirm reachability in the next step.

---

## 2. CLI → Windows via WSL2

There is **no native Windows build yet** (DSCP/ECN marking on Windows needs the
qWAVE API). The working path today is **WSL2**, which runs the real Linux binary.

1. Install WSL2 (PowerShell as admin, then reboot):
   ```powershell
   wsl --install
   ```
2. Open the "Ubuntu" app, then inside it copy the binary in and run it. If you
   saved `qoe-cli` to your Windows Downloads:
   ```bash
   cp /mnt/c/Users/YOU/Downloads/qoe-cli ~/qoe-cli && chmod +x ~/qoe-cli
   ```
3. **Wire the PC to the modem by Ethernet** (Wi-Fi confounds the DOCSIS
   measurement — this is why the wedge is wired-CLI-first).

### First: is WSL2 preserving the marks?

WSL2 NATs your traffic, which *may* rewrite the ECN/DSCP bits. Don't guess — the
tool measures it. Run the diagnostic (no load, just marked probes):

```bash
~/qoe-cli -server YOUR_VPS_IP:7700
```

Look at **`LL marking survival`** in the output:

- **~100 %** → WSL2 preserved the marks. You have a real test path; continue.
- **near 0 %** → WSL2 (or the path) stripped the marks. WSL2 is not viable here;
  use the fallback below.

### Then: the full run (role-switched)

```bash
~/qoe-cli -server YOUR_VPS_IP:7700 -run -down-tier-mbps 500 -up-tier-mbps 50 \
  -isp "YourISP" -region "your-area"
```

- default output = **field pass/fail** checklist.
- add `-engineer` for full telemetry (base RTT, marking %, LL-vs-classic
  percentiles).
- add `-submit http://DASHBOARD:8080/ingest` to record the run.

Set `-down-tier-mbps` / `-up-tier-mbps` to *your provisioned speeds* — the
capacity gate compares achieved vs provisioned to confirm your line is the
bottleneck.

### Fallback if WSL2 mangles the marks

Run `qoe-cli` on **any wired Linux machine** on the same modem — a spare laptop
booted to Linux, or a Raspberry Pi. Same binary (use the `arm64` build for a Pi),
same command. This sidesteps both the WSL2 NAT and the missing Windows port.

---

## 3. Optional: dashboard

The engineer view is optional and can run on the VPS or your machine:

```bash
qoe-dashboard -addr :8080        # in-memory store; open http://host:8080
```

For durable storage, apply `storage/schema.sql` to Postgres and wire the Postgres
adapter in `dashboard/cmd/qoe-dashboard` (a driver import + DSN — see that file's
header comment).

---

## Future: native Windows

A native Windows CLI needs DSCP/ECN egress marking via the qWAVE (Qos2) API and
admin/QoS-policy handling — a real chunk of work, tracked for after M0 validates
the approach. Until then: WSL2 (if marks survive) or a wired Linux device.
