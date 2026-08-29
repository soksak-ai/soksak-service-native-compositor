# Change log

This file records completed changes and their verification. Current rules are defined by the
README and the other documents in this directory.

## 2026-08-29

- The frontend observer can stage a host presentation inventory before the matching DOM commit.
  Staging shares the normal sequence and serialized writer and returns the real compositor receipt.
  Owner frontend tests and `make verify` passed.

## 2026-08-28

- DOM snapshots now conjoin a surface's own visibility with every ancestor
  `data-surface-visible` declaration. Inactive terminal surfaces can no longer cover active browser
  chrome; mutation observation commits both visibility edges.
- Compositor transactions no longer hold the state mutex while calling a native backend or
  resolving a window. Backend writes are serialized independently, which removes the AppKit main
  thread and compositor worker lock cycle. Go race tests and frontend observer tests passed.
- Build preflight now checks the effective pnpm version at the repository root. The prior check also
  inspected the globally installed package behind the launcher and rejected a valid
  `packageManager` selection. The owner test and `make verify` passed in an arm64 login shell.
