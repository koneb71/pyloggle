# System

**System** shows the health of the Pyloggle server itself: the database, the disk, what came
in and what was dropped. It is for administrators, and appears in the sidebar only for them.

![System: database, disk, incoming data, queues, alerts engine and storage per table](../images/screenshots/system-light.png#gh-light-mode-only)
![System: database, disk, incoming data, queues, alerts engine and storage per table](../images/screenshots/system-dark.png#gh-dark-mode-only)

| Part | What to look for |
|---|---|
| Database | Whether ClickHouse answers, its version, and open connections |
| Disk space used | How full ClickHouse's disk is. Shorten [retention](../self-hosting/retention-and-footprint.md) or add space before it fills up |
| Cleanup tasks running | Deletions ClickHouse is still working through, for example after a project was deleted |
| Incoming data | Events received per minute over the last hour, for all projects |
| Waiting to be saved | Events queued for the database, by kind. Full queues mean dropped data |
| What happened to incoming data | Since the server started: what was accepted, and what was dropped and why (sampled, over a rate limit, a full queue, over a storage limit) |
| Alerts engine | Whether alerts are on, and when the rules were last checked |
| Storage by table | Rows, size on disk, compression and the oldest day kept, per table |

The same figures are available to Prometheus at `GET /metrics`, and the server's version and
uptime are in the page's subtitle.
