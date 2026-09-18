# Camunda Stub Worker

A desktop stub worker for testing Camunda 8 processes locally. It activates Zeebe jobs, persists each activation in SQLite, and responds with a selected scenario either automatically or after manual review.

## Features

- Multiple local Camunda profiles with explicit `host:port`, plaintext gRPC, and no authentication.
- Topology checks with selected and detected server versions.
- Case-sensitive job types, automatic and manual modes, and named success, business-error, and technical-failure scenarios.
- Long polling, global and per-type limits, and capacity reservation before `ActivateJobs`.
- Immutable scenario snapshots for every activation, persisted before a command is sent.
- `UpdateJobTimeout` while jobs wait for manual input or an automatic delay.
- Conservative handling of ambiguous sends: a network break or deadline after possible transmission becomes `unknown` and is not retried automatically.
- Searchable, filterable, paginated history with send attempts.
- Crash recovery (`active` → `interrupted`, `sending` → `unknown`).
- JSON profile import and export with regenerated UUIDs and no history.
- Single-instance locking, SQLite WAL and foreign keys, and completed-history cleanup.
- English Svelte UI that keeps configuration and history available offline.
- Non-blocking update checks against published GitHub Releases, with an in-app download notification.
- CodeMirror-powered JSON editing with syntax highlighting and validation.

Zeebe keys are presented in the UI as decimal strings. JSON variables are stored and transmitted as their original text, so integers larger than `2^53` never pass through JavaScript `Number` or Go `float64`.

## Getting started

1. Start a local Zeebe instance, or run `docker compose -f integration/docker-compose.yml up -d`.
2. Start the application. On first launch it creates a **Local Camunda** profile for `localhost:26500` and Camunda `8.5`.
3. Click **Check connection**.
4. Add a job type that exactly matches the BPMN `zeebe:taskDefinition type`.
5. Create a scenario. In automatic mode, select it as the active scenario.
6. Start the job type or all configured job types. Deploy BPMN models and start process instances with an external tool.

In manual mode, activated jobs appear under **Awaiting response**. You can select any scenario for that job type, edit its local copy, and send it. Scenario delay is not applied in manual mode.

For a technical failure, `remainingRetries` is the absolute retry count sent to Zeebe. Reusing the same positive value may cause repeated activations.

## Application data

The SQLite database and instance lock are stored in the standard user configuration directory:

- macOS: `~/Library/Application Support/Camunda Stub Worker/`
- Windows: `%AppData%\Camunda Stub Worker\`
- Linux: `$XDG_CONFIG_HOME/Camunda Stub Worker/` or `~/.config/Camunda Stub Worker/`

The exact path is shown under **Connections**. The MVP does not store authentication credentials or other secrets. Profile exports contain profiles, job types, and scenarios; they exclude history and runtime state.

## Versions and system requirements

Dependencies are pinned in `go.mod` and `frontend/package-lock.json`:

- Go **1.25.0**
- Wails **2.15.0**
- Svelte **5.57.0**, Vite **8.3.0**, and TypeScript **5.9.2**
- Camunda Go client / Zeebe protocol **8.5.25**
- `modernc.org/sqlite` **1.39.1** (pure Go; no system SQLite dependency)

The verified server target is **Zeebe 8.5.25**. **Other 8.x** uses the 8.5 API mode and does not imply verified compatibility.

Wails 2.15 system requirements:

- Windows 10/11 x64 with WebView2 Runtime.
- macOS 10.13+ x64 or macOS 11+ arm64. Development requires Xcode Command Line Tools.
- Linux x64 with GTK3 and WebKit2GTK. Distributions using `libwebkit2gtk-4.1` require the `webkit2_41` build tag.

Release builds do not require Go or Node.js. Current artifacts are not publisher-signed or notarized; local macOS builds use Wails ad-hoc signing.

## Development

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
cd frontend && npm ci && cd ..
wails dev
```

Checks:

```bash
go test -race ./...
cd frontend && npm run check && npm run build
```

Production builds:

```bash
# macOS (the wrapper links the framework required by file dialogs)
wails build -clean -compiler ./scripts/go-wails-macos.sh

# Windows x64
wails build -clean -platform windows/amd64 -webview2 download

# Linux x64, WebKit2GTK 4.1
wails build -clean -platform linux/amd64 -tags webkit2_41
```

The macOS `.app` is written to `build/bin`. A Windows installer can be built with `-nsis` when NSIS is installed. `.github/workflows/build.yml` builds on native runners for each operating system.

## Integration environment

```bash
docker compose -f integration/docker-compose.yml up -d
docker compose -f integration/docker-compose.yml logs -f zeebe
```

Models in `integration/bpmn/`:

- `success.bpmn` — job type `stub-success`.
- `business-error.bpmn` — job type `stub-business-error`, boundary error code `BUSINESS_ERROR`.
- `technical-failure.bpmn` — job type `stub-technical-failure` with three initial retries.

Deploy models and start instances using an external tool such as Camunda Modeler or `zbctl`.

## Semantics and limitations

- Other workers for the same job type compete for jobs; exclusivity is not guaranteed.
- Zeebe does not issue an ownership token for a specific activation, so exactly-once handling is not guaranteed.
- If a command may have been transmitted before `DEADLINE_EXCEEDED`, a connection break, or shutdown, its result is marked `unknown` and cannot be retried from that history entry.
- A successful topology check proves reachability, not support for every operation. `UNIMPLEMENTED` for the required `UpdateJobTimeout` operation stops new activations.
- OAuth, TLS, SaaS, multitenancy, conditional scenarios, scripts, Operate API, BPMN deploy/start, and unattended installation of updates are outside the MVP scope.

## Verification

- `go test -race ./...` passes on macOS arm64.
- `npm run check` and the production frontend build pass.
- A Wails 2.15.0 macOS arm64 `.app` builds and receives an ad-hoc signature.
- `go test -tags integration -run TestZeebe825Smoke -v ./integration` passes against Zeebe 8.5.25, covering the three bundled models, a success result containing `9007199254740993`, business error, `FailJob`, and `UpdateJobTimeout`.
- Windows x64, macOS x64, and Linux x64 builds are delegated to native CI runners and have not been executed locally in this environment.
