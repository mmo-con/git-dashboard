# Security Policy

## Supported Versions

Security fixes are intended for the latest released version of headquarters.
Older versions may not receive security updates.

## Reporting a Vulnerability

Please report suspected security vulnerabilities privately through
[GitHub Security Advisories](https://github.com/mmo-con/headquarters/security/advisories/new).

Do not disclose vulnerabilities in public issues, discussions, or pull requests
before the maintainers have had an opportunity to investigate.

Please include, where possible:

- The affected headquarters version and operating system.
- A description of the vulnerability and its potential impact.
- Steps to reproduce, preferably with a minimal example.
- Any relevant configuration or logs, with credentials and sensitive data removed.

The maintainers will review the report and coordinate further investigation
and disclosure with you. Response and resolution times may vary.

## Security Considerations

headquarters invokes the installed Git CLI and uses your Git configuration,
credential helpers, and SSH configuration. It can also launch configured
external programs. Use trusted repositories, configuration files, and tools.

Release archives are accompanied by SHA-256 checksums. These checksums are
published alongside the archives and are not independent signatures.
