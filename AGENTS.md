# Repository instructions

## Language and paired documentation

- English is the default language for source code, comments, UI text, examples, and documentation.
- Maintain a Korean counterpart for every user-facing Markdown guide. The English file uses `.md`; its Korean counterpart uses `.kr.md` with the same basename and structure.
- Update both language versions in the same change. Keep commands, identifiers, option names, field names, and code literals exact rather than translating them.
- `README.md` and `README.kr.md` are a pair. `AGENTS.md`, generated files, licenses, and third-party material do not require a translated copy.

## Documentation ownership

- The [K-P2PLab Hub](https://github.com/k-p2p-lab/hub) owns project-wide and version-independent material: objectives, research context, design principles, conceptual architecture, publications, and high-level version history.
- This repository owns v3 implementation material: executable behavior, source code, component and network mapping, APIs, scenario fields, metrics, deployment and operations, validation, compatibility details, and current limitations.
- Put an abstract cross-version explanation in the Hub and link to the relevant v3 guide for concrete behavior. Put instructions or claims that depend on current v3 code in this repository and link back to the Hub when project context is useful.
- Keep `docs/architecture.md` and `docs/architecture.kr.md` aligned with the actual v3 implementation. Do not present the Hub's conceptual diagram as proof that every pictured box is a separate service or network.

## Windows validation safety

- Do not run native Windows Go tests or test binaries. This includes `go test`, `go test -c`, direct `*.test.exe` execution, and helper commands that invoke them on Windows.
- Do not run a Windows command when it is known or reasonably likely to open a security, firewall, elevation, permission, or executable-approval dialog. The user operates this workspace remotely and cannot interact with such dialogs.
- Validate executable behavior in a non-interactive Linux environment, such as the Docker `test` build target, `make test-linux`, CI, or a remote Linux host. Use static file inspection on Windows when no such Linux environment is already available.
- Never treat a Windows native test as a substitute for Linux validation of Docker namespaces, traffic control, Swarm networking, signals, permissions, or cleanup.
