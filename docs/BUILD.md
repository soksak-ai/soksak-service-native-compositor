# Build environment

`go.mod`, `.node-version`, and `package.json#packageManager` own the exact Go, Node, and pnpm
versions. The host shell provides native tools. The repository does not install a second toolchain,
inject a PATH, or select an ambient fallback.

On Apple Silicon, verification requires an arm64 login shell. Before a build:

```sh
arch
which node
node -v
```

The required result is `arm64`, `/opt/homebrew/bin/node`, and the version declared by
`.node-version`. `make verify` runs only after this condition holds.

pnpm is resolved at the consumer repository root. `package.json#packageManager` selects the
effective pnpm version, and `pnpm --version` at that root is the value checked by preflight. The
physical package behind the pnpm launcher is not a second version owner.

Registry and output locations are Make command-line arguments. They are not environment variables
and are not carried through `MAKEFLAGS`:

```sh
make verify REGISTRY=http://127.0.0.1:4873/
make publish OUT=/absolute/release REGISTRY=http://127.0.0.1:4873/
```

The second command is available only in repositories that own a publish target.
