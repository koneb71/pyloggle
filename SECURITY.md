# Security policy

## Supported versions

Security fixes go into the latest release of the current major version.

| Version | Supported |
|---|---|
| 1.x | Yes |
| 0.x | No. Upgrade to 1.x |

## Report a vulnerability

Please do not open a public issue, discussion or pull request for a security problem.

Report it privately through GitHub instead:
**[Report a vulnerability](https://github.com/koneb71/pyloggle/security/advisories/new)**. Only
the maintainers can see the report, and we can work on a fix with you in a private copy of the
repository.

Please include:

- what an attacker can do, and what they need first (an access key, a signed-in account,
  network access to the server, …);
- the steps to reproduce it, or a proof of concept;
- the version (`ingestd -version`, or `pip show pyloggle` for the Python package) and how it is
  installed (Docker image, release binary, built from source);
- any settings that matter, such as the sign-in mode.

## What happens next

This is a project maintained in spare time, so these are best-effort targets:

- We acknowledge the report within 7 days.
- We confirm or rule out the problem and tell you what we plan to do.
- We fix it, publish a release, and publish a security advisory that credits you, unless you
  prefer not to be named.

Please give us a reasonable time to release a fix before you talk about the problem publicly.

## Scope

In scope:

- the server (`ingestd`) and the app it serves;
- the Python package `pyloggle`;
- the Docker image, the Compose file and the release archives published from this repository.

Not in scope:

- a server run with sign-in off (`PYLOGGLE_UI_AUTH=disabled`) and reachable by other people:
  that setting gives everyone who can reach it full access, by design;
- problems in ClickHouse, the sign-in provider, or other software Pyloggle uses. Please report
  those to their own projects (tell us too if Pyloggle should work around them);
- denial of service that needs a valid access key with permission to send data.

The [security guide](https://koneb71.github.io/pyloggle/self-hosting/security) explains how to
run Pyloggle safely: sign-in, HTTPS, access keys and keeping ClickHouse private.
