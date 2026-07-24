# syntax=docker/dockerfile:1
# Development container image for FlowLens.
# Bundles both toolchains (Go + Node) plus the CLIs the Makefile targets use,
# so the whole monorepo can be developed from a single container.
FROM golang:1.26-bookworm

# --- OS packages ------------------------------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates git make curl gnupg postgresql-client \
    && rm -rf /var/lib/apt/lists/*

# --- Node.js 22 (matches apps/web/Dockerfile.dev) --------------------
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# --- GitHub CLI ---------------------------------------------------------
RUN mkdir -p -m 755 /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# --- Go dev tooling ---------------------------------------------------
# air: hot reload | sqlc: DB codegen | migrate: schema migrations
RUN go install github.com/air-verse/air@v1.62.0 \
 && go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest \
 && go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
# golangci-lint v2 (repo config is `version: "2"`).
RUN curl -fsSL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
      | sh -s -- -b /go/bin

# --- Claude Code CLI --------------------------------------------------
# Installed globally so it's available on the devcontainer terminal PATH.
RUN npm install -g @anthropic-ai/claude-code

# --- PATH for login shells -------------------------------------------
# The golang image exposes go + /go/bin via the Docker ENV PATH, but the
# devcontainer probes env with a login shell (`--default-user-env-probe
# loginInteractiveShell`), and /etc/profile resets PATH to the Debian
# default — dropping go/air/sqlc/migrate. Re-add them for login shells.
RUN printf 'export PATH="$PATH:/usr/local/go/bin:/go/bin"\n' > /etc/profile.d/10-go-path.sh

# --- Non-root user ----------------------------------------------------
ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=$USER_UID
RUN groupadd --gid $USER_GID $USERNAME \
 && useradd --uid $USER_UID --gid $USER_GID -m -s /bin/bash $USERNAME \
 && mkdir -p /go/pkg/mod /home/$USERNAME/.claude \
 && chown -R $USER_UID:$USER_GID /go /home/$USERNAME/.claude
USER $USERNAME
