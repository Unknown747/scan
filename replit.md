# Workspace

## Overview

pnpm workspace monorepo using TypeScript. Each package manages its own dependencies.

## Stack

- **Monorepo tool**: pnpm workspaces
- **Node.js version**: 24
- **Package manager**: pnpm
- **TypeScript version**: 5.9
- **API framework**: Express 5
- **Database**: PostgreSQL + Drizzle ORM
- **Validation**: Zod (`zod/v4`), `drizzle-zod`
- **API codegen**: Orval (from OpenAPI spec)
- **Build**: esbuild (CJS bundle)

## Key Commands

- `pnpm run typecheck` — full typecheck across all packages
- `pnpm run build` — typecheck + build all packages
- `pnpm --filter @workspace/api-spec run codegen` — regenerate API hooks and Zod schemas from OpenAPI spec
- `pnpm --filter @workspace/db run push` — push DB schema changes (dev only)
- `pnpm --filter @workspace/api-server run dev` — run API server locally

See the `pnpm-workspace` skill for workspace structure, TypeScript setup, and package details.

## ETH Wallet Scanner (Go)

Located at `eth-wallet-scanner/`. A Go tool to sequentially generate and check Ethereum wallets.

- **Language**: Go 1.21
- **Binary**: `eth-wallet-scanner/eth-scanner` (pre-built)
- **Build**: `cd eth-wallet-scanner && /nix/store/a90l6nxkqdlqxzgz5j958rz5gwygbamc-go-1.21.13/bin/go build -o eth-scanner .`
- **Packages**: `github.com/ethereum/go-ethereum/crypto`

### Key Files
- `eth-wallet-scanner/main.go` — CLI entry point, flags, scan/generate modes
- `eth-wallet-scanner/wallet/wallet.go` — Wallet generation from sequential index
- `eth-wallet-scanner/checker/checker.go` — Parallel balance checker with worker pool

### Usage
```bash
./eth-scanner -gen -start 1 -end 100          # Generate only
./eth-scanner -start 1 -end 1000 -workers 20  # Generate + check balance
```
