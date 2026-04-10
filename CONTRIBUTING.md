# Contributing to G-man

First off, thank you for considering contributing to G-man! It’s people like you who make it a great tool for the Steam automation community.

By contributing to this project, you agree to abide by its terms and follow the coding standards outlined below.

## 🟢 How Can I Contribute?

### Reporting Bugs

* **Check the Issues:** Search the existing issues to see if the bug has already been reported.
* **Provide Details:** Use a clear and descriptive title. Include steps to reproduce, the expected behavior, and what actually happened.
* **Environment:** Mention your Go version, OS, and any specific Steam environment details (e.g., "WebSockets connection in China realm").

### Suggesting Enhancements

* **Open an Issue:** Describe the feature you'd like to see and, most importantly, **why** it would be useful for the project.
* **Scope:** We prefer modular enhancements. If a feature is too specific to one game or use case, consider if it should be a separate package in `pkg/`.

### Pull Requests

* **Fork and Branch:** Create a branch from `main` with a descriptive name (e.g., `feat/market-history` or `fix/totp-alignment`).
* **Atomic Commits:** Keep your commits small and focused.
* **Tests:** Every new feature or bug fix **must** include corresponding tests.
* **Documentation:** Update the `README.md` or `doc.go` files if you are changing the public API.

---

## 🛠 Development Standards

G-man is built with a focus on high performance and maintainability. We follow standard Go idioms and some project-specific rules.

### 1. Code Style

* **Formatting:** All code must be formatted with `gofmt` (or `goimports`).
* **Linting:** We use `golangci-lint`. Please run it before submitting a PR.
* **Naming:** Use `camelCase` for internal variables and `PascalCase` for exported ones. Avoid stuttering (e.g., use `auth.Client` instead of `auth.AuthClient`).

### 2. Concurrency & State

* **No Global State:** Global variables are strictly forbidden. Use structs and pass dependencies through constructors.
* **Mutexes:** Keep critical sections as small as possible. Prefer `atomic` values for simple counters or state flags.
* **Contexts:** Always propagate `context.Context` through blocking calls (Network, I/O, Long-running loops).

### 3. Error Handling

* **Wrap Errors:** Use `fmt.Errorf("context: %w", err)` to provide trace-like context to errors.
* **Typed Errors:** For common failure modes (like "Rate Limit" or "Not Logged In"), use the predefined errors in `pkg/steam/api`.

### 4. Interfaces

* **Consumer-Defined:** Define interfaces where the logic is *consumed* as a dependency, not where it is implemented.
* **Small Interfaces:** Follow the Single Responsibility Principle. `io.Reader` is a great example—keep your interfaces lean.

### 5. Logging

* **Structured Only:** Do not use `fmt.Println` or the standard `log` package. Use the `pkg/log` package provided in the SDK.
* **Contextual Fields:** Always include relevant metadata using fields (e.g., `log.SteamID(id)`).

## 📦 Dependency Policy

We strive to keep the dependency tree as lean as possible.

* **Standard Library First:** Always prefer the Go standard library if it can achieve the task efficiently.
* **Justification:** Adding a new external dependency requires a strong justification in the PR description.
* **License Compatibility:** New dependencies must have a permissive license (MIT, BSD, Apache 2.0).

## 💬 Commit Messages

We follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

* `feat:` for new features (e.g., `feat(tf2): add automatic scrap combining`).
* `fix:` for bug fixes (e.g., `fix(socket): resolve race condition in heartbeat`).
* `docs:` for documentation changes.
* `refactor:` for code changes that neither fix a bug nor add a feature.

## 🧪 Testing

We rely heavily on automated testing to ensure the framework remains stable.

* **Mocking:** Use the `test/` package to mock network requesters and Steam CM responses.
* **Race Detector:** Always run your tests with the race detector enabled: `go test -race ./...`.

---

## 🔒 Security Vulnerabilities

If you discover a security vulnerability (especially related to Steam Guard or credential handling), please **do not open a public issue**. Instead, please contact the maintainers privately at `arsenii.komolov@yandex.ru` (or via Discord).

## ⚖️ License

By contributing, you agree that your contributions will be licensed under the project's **BSD-3-Clause License**.

---

<div align="center">
  <sub>Happy coding, and see you on the Market!</sub>
</div>
