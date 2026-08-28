# Change log

This file records completed changes and their verification. Current rules are defined by the
README and the other documents in this directory.

## 2026-08-28

- Compositor transactions no longer hold the state mutex while calling a native backend or
  resolving a window. Backend writes are serialized independently, which removes the AppKit main
  thread and compositor worker lock cycle. Go race tests and frontend observer tests passed.
- Build preflight now checks the effective pnpm version at the repository root. The prior check also
  inspected the globally installed package behind the launcher and rejected a valid
  `packageManager` selection. The owner test and `make verify` passed in an arm64 login shell.
